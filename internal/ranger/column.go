// Package ranger is the three-column miller-columns layout.
package ranger

import tea "github.com/charmbracelet/bubbletea"

// Column is one vertical strip. Each owns its state and is told by
// the Ranger when its parent's selection changes (which may need to
// trigger an async refetch).
type Column interface {
	Title() string

	// Init kicks off background work. May return nil.
	Init() tea.Cmd

	// Update processes one tea.Msg. (cmd, handled).
	Update(tea.Msg) (tea.Cmd, bool)

	// SetParentSelection is called whenever the upstream column's
	// Selected() changes. Returning a Cmd lets the column fire an
	// async fetch (e.g. read the new stream's events). For
	// already-loaded data the column can just stash the parent and
	// return nil.
	SetParentSelection(parent string) tea.Cmd

	// Selected — id of the highlighted row, or "" if empty.
	Selected() string

	// Move shifts selection by delta (negative = up). Bounded.
	Move(delta int)

	// SetFilter applies a case-insensitive substring filter to the
	// column's rows. Empty string clears it. Detail/render-only
	// columns can implement as a no-op.
	SetFilter(needle string)

	// GotoID jumps selection to the row whose id matches needle
	// (case-insensitive substring). Returns true on a hit; on miss
	// the selection is left unchanged. List-style columns implement
	// this; detail columns can no-op return false.
	GotoID(needle string) bool

	// View renders at (width, height). active = column is focused.
	View(width, height int, active bool) string

	// Stop releases background resources.
	Stop()
}
