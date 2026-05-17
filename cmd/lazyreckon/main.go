// lazyreckon — a terminal UI for the ReckonDB event store, in the
// spirit of lazygit / lazydocker / k9s.
//
// Repo: codeberg.org/reckon-db-org/reckon-lazy (binary stays
// `lazyreckon` to fit the lazy-* family).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/panes"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ui"
)

type model struct {
	endpoint string
	client   *reckon.Client

	tabs   []panes.Pane
	active int

	width, height int

	// tick fires every second to refresh "since" timestamps and
	// other time-derived labels.
	clock time.Time
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func newModel(endpoint string, c *reckon.Client) model {
	return model{
		endpoint: endpoint,
		client:   c,
		tabs: []panes.Pane{
			panes.NewStores(c),
			panes.NewPlaceholder("streams", "StreamService.ListStreams"),
			panes.NewPlaceholder("events", "SubscriptionService.Subscribe"),
			panes.NewPlaceholder("subscriptions", "SubscriptionService.{List,GetLag}"),
			panes.NewPlaceholder("snapshots", "SnapshotService.ListAllSnapshots"),
		},
		active: 0,
		clock:  time.Now(),
	}
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick()}
	for _, p := range m.tabs {
		if c := p.Init(); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m2 := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = m2.Width
		m.height = m2.Height
		return m, nil

	case tea.KeyMsg:
		switch m2.String() {
		case "q", "ctrl+c":
			m.shutdown()
			return m, tea.Quit
		case "tab", "right", "l":
			m.active = (m.active + 1) % len(m.tabs)
			return m, nil
		case "shift+tab", "left", "h":
			m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
			return m, nil
		case "1", "2", "3", "4", "5":
			idx := int(m2.String()[0] - '1')
			if idx < len(m.tabs) {
				m.active = idx
			}
			return m, nil
		}

	case tickMsg:
		m.clock = time.Time(m2)
		return m, tick()
	}

	// Fan messages out to every pane so background streams in
	// hidden tabs keep draining.
	var cmds []tea.Cmd
	for _, p := range m.tabs {
		if cmd, _ := p.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	w := m.width
	if w < 40 {
		w = 80
	}
	h := m.height
	if h < 10 {
		h = 24
	}

	header := ui.Header(m.endpoint, w)

	labels := make([]string, len(m.tabs))
	for i, p := range m.tabs {
		labels[i] = fmt.Sprintf("%d %s", i+1, p.Title())
	}
	tabBar := ui.Tabs(labels, m.active, w)

	// Pane content area sits between tab strip and status bar.
	paneInnerW := w - 4 // border + padding
	paneInnerH := h - 5 // header + tab + status + padding
	content := m.tabs[m.active].View(paneInnerW, paneInnerH)
	body := theme.Pane.Width(paneInnerW + 2).Render(content)

	hints := []ui.KeyHint{
		{Key: "1-5", Action: "jump"},
		{Key: "tab", Action: "next"},
		{Key: "q", Action: "quit"},
	}
	summary := fmt.Sprintf("%s · %s",
		theme.HeaderAccent.Inline(true).Render(theme.Glyph),
		m.clock.Format("15:04:05"))
	status := ui.StatusBar(hints, summary, w)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		tabBar,
		body,
		strings.Repeat(" ", 0), // breathing room
		status,
	)
}

func (m model) shutdown() {
	for _, p := range m.tabs {
		p.Stop()
	}
	if m.client != nil {
		_ = m.client.Close()
	}
}

func main() {
	endpoint := flag.String("endpoint", "localhost:50051",
		"reckon-gateway gRPC endpoint (host:port)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := reckon.Connect(ctx, *endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lazyreckon: connect %s: %v\n", *endpoint, err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(*endpoint, c), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "lazyreckon: %v\n", err)
		os.Exit(1)
	}
}
