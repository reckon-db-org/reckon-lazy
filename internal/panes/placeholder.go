package panes

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// Placeholder is a stub pane shown for tabs not yet wired against
// reckon-go. Each one names the service it'll consume so the user
// knows what's coming.
type Placeholder struct {
	title  string
	source string
}

func NewPlaceholder(title, source string) *Placeholder {
	return &Placeholder{title: title, source: source}
}

func (p *Placeholder) Title() string                       { return p.title }
func (p *Placeholder) Init() tea.Cmd                       { return nil }
func (p *Placeholder) Update(tea.Msg) (tea.Cmd, bool)      { return nil, false }
func (p *Placeholder) Stop()                               {}

func (p *Placeholder) View(width, height int) string {
	title := theme.PaneTitle.Render(p.title)
	body := theme.RowDim.Render(fmt.Sprintf(
		"not wired yet — will consume %s once the reckon-go wrapper lands.",
		theme.RowValue.Render(p.source)))
	return title + "\n" + body
}
