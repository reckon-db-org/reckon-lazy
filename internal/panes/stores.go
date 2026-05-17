// Package panes contains one model+view per top-level lazyreckon
// tab. Each pane owns its own state and message types; the top-level
// model in cmd/lazyreckon routes tea.Msg to the active pane.
package panes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/stores"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// Stores is the topology pane: a live list of (store_id, node)
// registrations from the gateway, refreshed by the WatchStores
// streaming RPC. The first snapshot is rendered immediately on
// connect; subsequent rows update in place as nodes
// announce/retire.
type Stores struct {
	client *reckon.Client
	stream *stores.Client

	// Cancels the watch goroutine on Stop.
	cancel context.CancelFunc

	// State observed via events.
	insts  map[string]stores.Instance // key = StoreID + "@" + Node
	err    error
	live   bool
	events int
}

// NewStores builds the pane bound to an open reckon.Client. The
// watch goroutine starts when Init's Cmd fires.
func NewStores(c *reckon.Client) *Stores {
	return &Stores{
		client: c,
		stream: c.Stores(),
		insts:  map[string]stores.Instance{},
	}
}

// Title — pane heading shown above the table.
func (s *Stores) Title() string { return "stores" }

// Init kicks off the WatchStores subscription. The returned Cmd
// blocks on the next event in the watch channel; once that fires,
// it's re-issued from Update so the model keeps draining the stream.
func (s *Stores) Init() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	events, errs := s.stream.Watch(ctx, stores.WithSnapshot(true))
	return tea.Batch(
		nextStoresEvent(events, errs),
		watchStateMsg{live: true}.fire(),
	)
}

// Stop releases the underlying stream goroutine. Call from the
// parent model when shutting down or when the pane is replaced.
func (s *Stores) Stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// Update routes pane-local messages.
func (s *Stores) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case storesEventMsg:
		key := m.event.Instance.StoreID + "@" + m.event.Instance.Node
		switch m.event.Type {
		case stores.EventRetired:
			delete(s.insts, key)
		default:
			s.insts[key] = m.event.Instance
		}
		s.events++
		// Re-arm the read.
		return nextStoresEvent(m.events, m.errs), true

	case storesErrMsg:
		s.err = m.err
		s.live = false
		return nil, true

	case watchStateMsg:
		s.live = m.live
		return nil, true
	}
	return nil, false
}

// View renders the pane body. width is the inner content width
// (excludes the surrounding border + padding); height likewise.
func (s *Stores) View(width, height int) string {
	header := theme.PaneTitle.Render(fmt.Sprintf(
		"%s · %s ·", "topology", s.summary()))

	if s.err != nil {
		return header + "\n" + theme.BadgeError.Render("error: ") +
			theme.RowValue.Render(s.err.Error())
	}

	if len(s.insts) == 0 {
		hint := theme.RowDim.Render("waiting for the first announcement…")
		return header + "\n" + hint
	}

	rows := s.sortedRows()
	cols := []column{
		{title: "store_id", width: 24, get: func(i stores.Instance) string { return i.StoreID }},
		{title: "node", width: 32, get: func(i stores.Instance) string { return i.Node }},
		{title: "mode", width: 9, get: func(i stores.Instance) string { return string(i.Mode) }},
		{title: "since", width: 12, get: func(i stores.Instance) string { return humanAgo(i.RegisteredAt) }},
		{title: "data_dir", width: 0, get: func(i stores.Instance) string { return i.DataDir }},
	}

	headerRow := renderColumns(cols, theme.RowHeader)
	bodyRows := make([]string, 0, len(rows))
	for _, inst := range rows {
		bodyRows = append(bodyRows, renderInstance(cols, inst))
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		headerRow,
		strings.Repeat("─", min(width-2, 88)),
		strings.Join(bodyRows, "\n"),
	)

	return header + "\n" + body
}

// summary — short status string for the title line.
func (s *Stores) summary() string {
	dot := theme.BadgeOK.Render("●")
	state := "live"
	if !s.live {
		dot = theme.BadgeError.Render("●")
		state = "disconnected"
	}
	unique := map[string]struct{}{}
	for _, i := range s.insts {
		unique[i.StoreID] = struct{}{}
	}
	return fmt.Sprintf("%s %s · %d store(s) · %d instance(s) · %d event(s)",
		dot, state, len(unique), len(s.insts), s.events)
}

func (s *Stores) sortedRows() []stores.Instance {
	out := make([]stores.Instance, 0, len(s.insts))
	for _, i := range s.insts {
		out = append(out, i)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StoreID != out[j].StoreID {
			return out[i].StoreID < out[j].StoreID
		}
		return out[i].Node < out[j].Node
	})
	return out
}

//------------------------------------------------------------------------------
// Messages

type storesEventMsg struct {
	event  stores.Event
	events <-chan stores.Event
	errs   <-chan error
}

type storesErrMsg struct {
	err error
}

type watchStateMsg struct {
	live bool
}

func (m watchStateMsg) fire() tea.Cmd {
	return func() tea.Msg { return m }
}

// nextStoresEvent waits for the next event or err on the watch
// channels and converts it into a tea.Msg.
func nextStoresEvent(events <-chan stores.Event, errs <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev, ok := <-events:
			if !ok {
				// stream closed — drain err channel for the cause.
				if err := <-errs; err != nil {
					return storesErrMsg{err: err}
				}
				return watchStateMsg{live: false}
			}
			return storesEventMsg{event: ev, events: events, errs: errs}
		case err := <-errs:
			if err != nil {
				return storesErrMsg{err: err}
			}
			return watchStateMsg{live: false}
		}
	}
}

//------------------------------------------------------------------------------
// Table rendering helpers

type column struct {
	title string
	width int // 0 = fill remaining
	get   func(stores.Instance) string
}

func renderColumns(cols []column, style lipgloss.Style) string {
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		w := c.width
		if w == 0 {
			w = 36
		}
		parts = append(parts, style.Render(pad(c.title, w)))
	}
	return strings.Join(parts, " ")
}

func renderInstance(cols []column, inst stores.Instance) string {
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		val := c.get(inst)
		w := c.width
		if w == 0 {
			w = 36
		}
		style := theme.RowValue
		// Subtle accents for known-meaningful values
		switch c.title {
		case "store_id":
			style = theme.RowKey
		case "mode":
			if inst.Mode == stores.ModeCluster {
				style = theme.BadgeOK
			} else {
				style = theme.BadgeWarn
			}
		case "since":
			style = theme.RowDim
		case "data_dir":
			style = theme.RowDim
		}
		parts = append(parts, style.Render(pad(val, w)))
	}
	return strings.Join(parts, " ")
}

func pad(s string, w int) string {
	if len(s) >= w {
		if w > 1 {
			return s[:w-1] + "…"
		}
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t).Truncate(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
