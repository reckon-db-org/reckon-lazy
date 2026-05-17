package panes

import tea "github.com/charmbracelet/bubbletea"

// Pane is what the top-level model holds in its tab slot. Each pane
// owns its own state and message types; the top-level model routes
// tea.Msg through Update and asks for a View at render time.
type Pane interface {
	Title() string
	Init() tea.Cmd
	// Update returns (cmd, handled). If handled is false the
	// top-level model may apply default behaviour to the message.
	Update(tea.Msg) (tea.Cmd, bool)
	View(width, height int) string
	// Stop releases any background goroutines / streams. Called on
	// app shutdown.
	Stop()
}
