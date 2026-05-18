// Minimal repro of the doubled-bar issue: header + status bar with a
// ticking clock, exactly the same chrome shape lazyreckon uses.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type model struct {
	w, h  int
	clock time.Time
}

func (m model) Init() tea.Cmd { return tickCmd() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m2 := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = m2.Width, m2.Height
		return m, nil
	case tickMsg:
		m.clock = time.Time(m2)
		return m, tickCmd()
	case tea.KeyMsg:
		if m2.String() == "q" || m2.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

var (
	bar     = lipgloss.NewStyle().Background(lipgloss.Color("57")).Foreground(lipgloss.Color("231")).Padding(0, 1)
	keyTag  = lipgloss.NewStyle().Background(lipgloss.Color("57")).Foreground(lipgloss.Color("46")).Bold(true)
)

// statusBar mimics lazyreckon's: many small styled segments concatenated
// into one line, padded with a gap, ending with the timestamp on the right.
func statusBar(width int, clock string) string {
	hints := []struct{ k, v string }{
		{"j/k", "move"}, {"h/l", "in/out"}, {"tab", "swap"},
		{"1-4", "mode"}, {"/", "filter"}, {":", "goto"},
		{"e", "edit"}, {"r", "refresh"}, {"?", "help"}, {"q", "quit"},
	}
	var left strings.Builder
	for i, h := range hints {
		if i > 0 {
			left.WriteString(bar.Render("  "))
		}
		left.WriteString(keyTag.Render(h.k))
		left.WriteString(bar.Render(" " + h.v))
	}
	right := bar.Render("● · " + clock)
	leftStr := left.String()
	gap := width - lipgloss.Width(leftStr) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return leftStr + bar.Render(strings.Repeat(" ", gap)) + right
}

// body mimics lazyreckon's stores mode: 4 rounded-bordered panes in 2x2.
func body(w, h int) string {
	topH := h / 2
	botH := h - topH
	innerW := w/2 - 1
	box := func(W, H int, title string) string {
		return lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			Padding(0, 1).
			Width(W).
			Height(H - 2).
			Render(title + "\nrow 1\nrow 2")
	}
	tl := box(innerW, topH, "TOP-LEFT")
	tr := box(w-innerW-1, topH, "TOP-RIGHT")
	bl := box(innerW, botH, "BOT-LEFT")
	br := box(w-innerW-1, botH, "BOT-RIGHT")
	top := lipgloss.JoinHorizontal(lipgloss.Top, tl, " ", tr)
	bot := lipgloss.JoinHorizontal(lipgloss.Top, bl, " ", br)
	return lipgloss.JoinVertical(lipgloss.Left, top, bot)
}

func (m model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}
	header := bar.Width(m.w).Render("HEADER " + m.clock.Format("15:04:05"))
	modeStrip := bar.Width(m.w).Render("MODE")
	bd := body(m.w, m.h-3)
	st := statusBar(m.w, m.clock.Format("15:04:05"))
	frame := lipgloss.JoinVertical(lipgloss.Left, header, modeStrip, bd, st)
	fmt.Fprintf(os.Stderr, "frame w=%d h=%d total=%d lastChar=%q\n",
		m.w, m.h, strings.Count(frame, "\n")+1, frame[len(frame)-3:])
	return frame
}

func main() {
	p := tea.NewProgram(model{}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
