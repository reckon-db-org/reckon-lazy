package ranger

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Crumber is an optional Column extension. A column that can label
// its current selection for the breadcrumb implements Crumb(). The
// Drill falls back to Title() for columns that don't implement it,
// and skips empty crumbs entirely (a leaf detail pane returns "").
type Crumber interface {
	Crumb() string
}

// Brancher is an optional Column extension for dynamic, unbounded
// drilling (e.g. walking a causation graph). A column at the end of
// the chain that can produce a child for its current selection
// implements Child(); pressing `l`/enter there pushes the child onto
// the stack. Returns (nil, false) when there is nothing to drill into.
type Brancher interface {
	Child() (Column, bool)
}

// Drill is a breadcrumb-style drill-down over the same Column chain
// the Ranger drives, but it shows at most two panes — the focused
// column as a slim sibling rail plus its child as a wide preview —
// and collapses to a single full-width pane at the leaf (focus ==
// last). Ancestors live in the breadcrumb (see Crumbs) instead of
// extra columns, so the detail pane gets the width the payload needs.
//
// The column chain IS the hierarchy: cols[0] → cols[1] → … → leaf.
// Parent→child selection propagates exactly as in Ranger; the focus
// index doubles as the drill depth.
type Drill struct {
	cols  []Column
	focus int
	// base is the length of the fixed chain passed to NewDrill.
	// Columns at index >= base were pushed dynamically (Brancher) and
	// are popped — and Stop()ed — when you drill back out of them.
	base int
}

// NewDrill builds a Drill over an ordered column chain. cols[0] is
// the root list; the last column is the leaf, rendered full-width
// when focused. The chain length becomes the base: Brancher pushes
// extend past it and pop back to it.
func NewDrill(cols ...Column) *Drill {
	return &Drill{cols: cols, focus: 0, base: len(cols)}
}

// Push appends a dynamically-built column (see Brancher), focuses it,
// and returns its Init batched with a propagate so it fetches and
// receives its parent selection.
func (d *Drill) Push(col Column) tea.Cmd {
	d.cols = append(d.cols, col)
	d.focus = len(d.cols) - 1
	return tea.Batch(col.Init(), d.propagate())
}

// Focused returns the column the cursor is currently on. Used by
// callers that need the typed selection of the deepest level (e.g.
// the editor handoff on a causation node).
func (d *Drill) Focused() Column { return d.cols[d.focus] }

// Init fans Init() out to every column.
func (d *Drill) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(d.cols))
	for _, c := range d.cols {
		if cmd := c.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// HandleKey: j/k move within the focused column; l/enter drills in
// (deeper, until the leaf); h drills back out; g/G jump to ends.
// (cmd, handled).
func (d *Drill) HandleKey(key string) (tea.Cmd, bool) {
	last := len(d.cols) - 1
	switch key {
	case "j", "down":
		d.cols[d.focus].Move(+1)
		return d.propagate(), true
	case "k", "up":
		d.cols[d.focus].Move(-1)
		return d.propagate(), true
	case "l", "right", "enter":
		if d.focus < last {
			d.focus++
			return d.propagate(), true
		}
		// At the last column: branch deeper if it can (dynamic push,
		// e.g. a causation node yielding the selected neighbour).
		if br, ok := d.cols[d.focus].(Brancher); ok {
			if child, ok := br.Child(); ok {
				return d.Push(child), true
			}
		}
		return nil, true
	case "h", "left":
		switch {
		case d.focus >= d.base:
			// Pop a dynamically-pushed column and release it.
			d.cols[d.focus].Stop()
			d.cols = d.cols[:d.focus]
			d.focus--
		case d.focus > 0:
			d.focus--
		}
		return nil, true
	case "g", "home":
		d.cols[d.focus].Move(-1 << 30)
		return d.propagate(), true
	case "G", "end":
		d.cols[d.focus].Move(1 << 30)
		return d.propagate(), true
	}
	return nil, false
}

// Update routes a tea.Msg through every column, then re-propagates.
func (d *Drill) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range d.cols {
		if cmd, _ := c.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := d.propagate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// propagate pushes each column's Selected() down to its child. Leaf
// detail columns read their parent's typed selection through a
// closure captured at construction, so they need nothing here, but
// driving propagate keeps RPC-fetching children (events, versions,
// lag) in sync on every move.
func (d *Drill) propagate() tea.Cmd {
	var cmds []tea.Cmd
	for i := 1; i < len(d.cols); i++ {
		if c := d.cols[i].SetParentSelection(d.cols[i-1].Selected()); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

// SetFilter applies (or clears) a filter on the focused column, then
// re-propagates so the child preview follows the new selection.
func (d *Drill) SetFilter(needle string) tea.Cmd {
	d.cols[d.focus].SetFilter(needle)
	return d.propagate()
}

// GotoID jumps the focused column to needle. Returns (cmd, hit).
func (d *Drill) GotoID(needle string) (tea.Cmd, bool) {
	if !d.cols[d.focus].GotoID(needle) {
		return nil, false
	}
	return d.propagate(), true
}

// FocusedSelection — id of the row highlighted in the focused column.
func (d *Drill) FocusedSelection() string {
	return d.cols[d.focus].Selected()
}

// Stop releases background resources across all columns.
func (d *Drill) Stop() {
	for _, c := range d.cols {
		c.Stop()
	}
}

// Crumbs returns the breadcrumb labels for the path from the root to
// the focused level. A column implementing Crumber supplies its own
// label; others fall back to Title(); empty labels are skipped (so a
// leaf detail pane contributes nothing and the path ends at its
// parent's selection).
func (d *Drill) Crumbs() []string {
	out := make([]string, 0, d.focus+1)
	for i := 0; i <= d.focus && i < len(d.cols); i++ {
		label := d.cols[i].Title()
		if cr, ok := d.cols[i].(Crumber); ok {
			label = cr.Crumb()
		}
		if label != "" {
			out = append(out, label)
		}
	}
	return out
}

// View renders the hybrid layout: [focused rail | child preview] at
// list levels, or the focused column full-width at the leaf.
func (d *Drill) View(width, height int) string {
	last := len(d.cols) - 1
	if d.focus >= last {
		// Leaf: the payload gets the whole width.
		return renderColumn(d.cols[last], width, height, true)
	}
	inner := width - 1 // one-column gap between rail and preview
	railW := inner * 32 / 100
	if railW < 20 {
		railW = 20
	}
	if railW > 44 {
		railW = 44
	}
	if railW > inner-10 {
		railW = inner / 2
	}
	return joinHoriz(
		renderColumn(d.cols[d.focus], railW, height, true),
		renderColumn(d.cols[d.focus+1], inner-railW, height, false),
	)
}
