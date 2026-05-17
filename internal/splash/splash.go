// Package splash is the startup screen: a list of saved cluster
// profiles with j/k navigation and inline forms for add/rename.
// Returns the chosen Profile (or empty + Cancelled) via Result().
package splash

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/genproto/gatewayv1"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/profiles"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ui"
)

// Result is what the picker returns after the user picks something
// (or quits).
type Result struct {
	Selected  *profiles.Profile // nil if cancelled
	Cancelled bool
}

// Model is the bubbletea model for the splash picker.
type Model struct {
	store *profiles.Store

	// Selection
	cursor int

	// Per-profile test result. Keyed by profile name.
	tests map[string]testResult

	// Mode: list | adding | renaming | confirmDelete
	mode   mode
	form   form
	notice string // transient one-line status (e.g. error from last action)

	// Final result, set right before we Quit.
	result Result

	width, height int
}

type mode int

const (
	modeList mode = iota
	modeAdding
	modeRenaming
	modeConfirmDelete
)

// New builds a splash picker over the given store. Sort by recency
// up-front so the most recently used profile is selected by default.
func New(store *profiles.Store) *Model {
	store.SortByRecency()
	return &Model{
		store: store,
		tests: map[string]testResult{},
	}
}

// Result is what the picker decided. Read after p.Run() returns.
func (m *Model) Result() Result { return m.result }

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(v)

	case testResultMsg:
		m.tests[v.name] = testResult{
			ok:     v.err == nil,
			err:    v.err,
			at:     time.Now(),
			detail: v.detail,
		}
		return m, nil
	}

	if m.mode == modeAdding || m.mode == modeRenaming {
		var cmd tea.Cmd
		m.form.name, cmd = m.form.name.Update(msg)
		var cmd2 tea.Cmd
		m.form.endpoint, cmd2 = m.form.endpoint.Update(msg)
		return m, tea.Batch(cmd, cmd2)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeAdding, modeRenaming:
		return m.handleFormKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmKey(msg)
	}

	// List mode
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		m.result = Result{Cancelled: true}
		return m, tea.Quit

	case "j", "down":
		if m.cursor < len(m.store.Profiles)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(0, len(m.store.Profiles)-1)

	case "enter":
		if p, ok := m.selected(); ok {
			m.result = Result{Selected: &p}
			return m, tea.Quit
		}

	case "n":
		m.startForm(form{
			name:     newInput("name (e.g. beam-lab)", "", 40),
			endpoint: newInput("endpoint (host:port)", "", 40),
			focused:  0,
		})
		m.mode = modeAdding

	case "r":
		if p, ok := m.selected(); ok {
			m.startForm(form{
				name:     newInput("new name", p.Name, 40),
				endpoint: newInput("endpoint", p.Endpoint, 40),
				focused:  0,
				original: p.Name,
			})
			m.mode = modeRenaming
		}

	case "d":
		if _, ok := m.selected(); ok {
			m.mode = modeConfirmDelete
		}

	case "t":
		if p, ok := m.selected(); ok {
			m.tests[p.Name] = testResult{pending: true}
			return m, runTest(p.Name, p.Endpoint)
		}
	}
	return m, nil
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeList
		m.notice = ""
		return m, nil
	case "tab", "shift+tab":
		m.form.toggleFocus()
		return m, nil
	case "enter":
		return m.submitForm()
	}
	return m, m.form.update(msg)
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if p, ok := m.selected(); ok {
			if err := m.store.Delete(p.Name); err != nil {
				m.notice = "delete: " + err.Error()
			} else if err := m.store.Save(); err != nil {
				m.notice = "save: " + err.Error()
			} else {
				m.cursor = clamp(m.cursor, 0, max(0, len(m.store.Profiles)-1))
				delete(m.tests, p.Name)
				m.notice = "deleted " + p.Name
			}
		}
		m.mode = modeList
	case "n", "N", "esc":
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.form.name.Value())
	ep := strings.TrimSpace(m.form.endpoint.Value())
	if err := profiles.ValidateName(name); err != nil {
		m.notice = "name: " + err.Error()
		return m, nil
	}
	if err := profiles.ValidateEndpoint(ep); err != nil {
		m.notice = "endpoint: " + err.Error()
		return m, nil
	}

	switch m.mode {
	case modeAdding:
		if err := m.store.Add(profiles.Profile{Name: name, Endpoint: ep}); err != nil {
			m.notice = "add: " + err.Error()
			return m, nil
		}
		if err := m.store.Save(); err != nil {
			m.notice = "save: " + err.Error()
			return m, nil
		}
		m.notice = "added " + name
		// Move cursor to the new entry.
		for i, p := range m.store.Profiles {
			if p.Name == name {
				m.cursor = i
				break
			}
		}

	case modeRenaming:
		if name != m.form.original {
			if err := m.store.Rename(m.form.original, name); err != nil {
				m.notice = "rename: " + err.Error()
				return m, nil
			}
		}
		// Find the (possibly renamed) profile and update endpoint
		// if changed.
		for i, p := range m.store.Profiles {
			if p.Name == name {
				m.store.Profiles[i].Endpoint = ep
				m.cursor = i
				break
			}
		}
		if err := m.store.Save(); err != nil {
			m.notice = "save: " + err.Error()
			return m, nil
		}
		m.notice = "saved " + name
	}

	m.mode = modeList
	return m, nil
}

func (m *Model) startForm(f form) {
	f.name.Focus()
	m.form = f
}

func (m *Model) selected() (profiles.Profile, bool) {
	if m.cursor < 0 || m.cursor >= len(m.store.Profiles) {
		return profiles.Profile{}, false
	}
	return m.store.Profiles[m.cursor], true
}

//------------------------------------------------------------------------------
// View

func (m *Model) View() string {
	w := m.width
	if w < 40 {
		w = 80
	}
	h := m.height
	if h < 10 {
		h = 24
	}

	header := ui.Header("", "—", ui.Health{}, w)

	titleArt := theme.PaneTitle.Render(
		fmt.Sprintf("%s  pick a cluster", theme.Glyph))

	var body string
	switch m.mode {
	case modeAdding:
		body = m.viewForm("add profile")
	case modeRenaming:
		body = m.viewForm("edit profile")
	case modeConfirmDelete:
		body = m.viewConfirm()
	default:
		body = m.viewList(w - 4)
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, titleArt, "", body)
	pane := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.HorusBright).
		Padding(1, 2).
		Width(w - 2).
		Render(inner)

	hints := m.statusHints()
	summary := time.Now().Format("15:04:05")
	if m.notice != "" {
		summary = m.notice
	}
	status := ui.StatusBar(hints, summary, w)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", pane, "", status)
}

func (m *Model) statusHints() []ui.KeyHint {
	switch m.mode {
	case modeAdding, modeRenaming:
		return []ui.KeyHint{
			{Key: "tab", Action: "next field"},
			{Key: "enter", Action: "save"},
			{Key: "esc", Action: "cancel"},
		}
	case modeConfirmDelete:
		return []ui.KeyHint{
			{Key: "y", Action: "delete"},
			{Key: "n", Action: "cancel"},
		}
	default:
		return []ui.KeyHint{
			{Key: "j/k", Action: "move"},
			{Key: "enter", Action: "connect"},
			{Key: "n", Action: "new"},
			{Key: "r", Action: "rename"},
			{Key: "d", Action: "delete"},
			{Key: "t", Action: "test"},
			{Key: "q", Action: "quit"},
		}
	}
}

func (m *Model) viewList(w int) string {
	if len(m.store.Profiles) == 0 {
		return theme.RowDim.Render(
			"no profiles yet — press ") +
			theme.BadgeOK.Render("n") +
			theme.RowDim.Render(" to add one")
	}

	headerLine := theme.RowHeader.Render(fmt.Sprintf("  %-22s %-26s %-10s  %s",
		"name", "endpoint", "last used", "test"))

	rows := make([]string, 0, len(m.store.Profiles))
	for i, p := range m.store.Profiles {
		rows = append(rows, m.renderRow(p, i == m.cursor))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerLine,
		theme.RowDim.Render(strings.Repeat("─", min(w, 72))),
		strings.Join(rows, "\n"),
	)
}

func (m *Model) renderRow(p profiles.Profile, selected bool) string {
	cursor := "  "
	nameStyle := theme.RowValue
	if selected {
		cursor = theme.BadgeOK.Render("▸ ")
		nameStyle = theme.RowKey
	}

	last := "—"
	if !p.LastUsed.IsZero() {
		last = humanAgo(p.LastUsed)
	}

	test := "—"
	tr, ok := m.tests[p.Name]
	switch {
	case !ok:
		test = theme.RowDim.Render("—")
	case tr.pending:
		test = theme.BadgeWarn.Render("…")
	case tr.ok:
		test = theme.BadgeOK.Render("● " + tr.detail)
	default:
		msg := tr.err.Error()
		if len(msg) > 18 {
			msg = msg[:17] + "…"
		}
		test = theme.BadgeError.Render("● " + msg)
	}

	return cursor + fmt.Sprintf("%s %s %s  %s",
		nameStyle.Render(padRight(p.Name, 22)),
		theme.RowValue.Render(padRight(p.Endpoint, 26)),
		theme.RowDim.Render(padRight(last, 10)),
		test)
}

func (m *Model) viewForm(title string) string {
	t := theme.PaneTitle.Render(title)
	nameField := theme.RowHeader.Render("name      ") + " " + m.form.name.View()
	epField := theme.RowHeader.Render("endpoint  ") + " " + m.form.endpoint.View()
	return lipgloss.JoinVertical(lipgloss.Left, t, "", nameField, epField)
}

func (m *Model) viewConfirm() string {
	p, _ := m.selected()
	return theme.BadgeError.Render("delete ") +
		theme.RowKey.Render(p.Name) +
		theme.RowDim.Render(" ("+p.Endpoint+")? ") +
		theme.RowDim.Render("y / n")
}

//------------------------------------------------------------------------------
// Form helpers

type form struct {
	name     textinput.Model
	endpoint textinput.Model
	focused  int // 0 = name, 1 = endpoint
	original string
}

func newInput(placeholder, initial string, width int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.SetValue(initial)
	in.CharLimit = 128
	in.Width = width
	in.Prompt = "› "
	return in
}

func (f *form) toggleFocus() {
	f.focused = 1 - f.focused
	if f.focused == 0 {
		f.endpoint.Blur()
		f.name.Focus()
	} else {
		f.name.Blur()
		f.endpoint.Focus()
	}
}

func (f *form) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if f.focused == 0 {
		f.name, cmd = f.name.Update(msg)
	} else {
		f.endpoint, cmd = f.endpoint.Update(msg)
	}
	return cmd
}

//------------------------------------------------------------------------------
// Test connection

type testResult struct {
	pending bool
	ok      bool
	err     error
	at      time.Time
	detail  string
}

type testResultMsg struct {
	name   string
	err    error
	detail string
}

func runTest(name, endpoint string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, err := reckon.Connect(ctx, endpoint)
		if err != nil {
			return testResultMsg{name: name, err: err}
		}
		defer c.Close()
		hc := gatewayv1.NewHealthServiceClient(c.Conn())
		resp, err := hc.GetServerInfo(ctx, &gatewayv1.GetServerInfoRequest{
			StoreId: "default_store",
		})
		if err != nil {
			return testResultMsg{name: name, err: err}
		}
		return testResultMsg{
			name:   name,
			detail: fmt.Sprintf("gw %s · db %s",
				resp.GetReckonGatewayVersion(),
				resp.GetReckonDbVersion()),
		}
	}
}

//------------------------------------------------------------------------------
// Utilities

func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	default:
		return t.Format("2006-01-02")
	}
}

func padRight(s string, w int) string {
	if len(s) >= w {
		if w > 1 {
			return s[:w-1] + "…"
		}
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
