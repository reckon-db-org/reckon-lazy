package ranger

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// Ranger holds a list of columns (2 or 3) and drives the
// miller-columns layout, focus, and parent→child selection
// propagation.
//
// Construction order matters: cols[0] feeds cols[1] feeds cols[2]
// (if present). Cols 1+ read their parent's Selected() value
// through SetParentSelection — typically driven from Update once
// focus or selection changes.
type Ranger struct {
	cols  []Column
	focus int
}

// New3 builds a 3-column Ranger. cols[0] is leftmost.
func New3(left, middle, right Column) *Ranger {
	return &Ranger{cols: []Column{left, middle, right}, focus: 0}
}

// New2 builds a 2-column Ranger. Useful for modes where the leaf
// of column 1 already names the row uniquely and column 2 is the
// detail view (e.g. cluster: nodes → detail).
func New2(left, right Column) *Ranger {
	return &Ranger{cols: []Column{left, right}, focus: 0}
}

// New is preserved as a 3-col alias for callers that haven't been
// migrated yet.
func New(left, middle, right Column) *Ranger { return New3(left, middle, right) }

// Init fans Init() out to all columns.
func (r *Ranger) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(r.cols))
	for _, c := range r.cols {
		if cmd := c.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// HandleKey processes a navigation key. (cmd, handled).
func (r *Ranger) HandleKey(key string) (tea.Cmd, bool) {
	last := len(r.cols) - 1
	switch key {
	case "j", "down":
		r.cols[r.focus].Move(+1)
		return r.propagate(), true
	case "k", "up":
		r.cols[r.focus].Move(-1)
		return r.propagate(), true
	case "h", "left":
		if r.focus > 0 {
			r.focus--
		}
		return nil, true
	case "l", "right", "enter":
		if r.focus < last {
			r.focus++
		}
		return nil, true
	case "g", "home":
		r.cols[r.focus].Move(-1 << 30)
		return r.propagate(), true
	case "G", "end":
		r.cols[r.focus].Move(1 << 30)
		return r.propagate(), true
	}
	return nil, false
}

// Update routes a tea.Msg through every column.
func (r *Ranger) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range r.cols {
		if cmd, _ := c.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := r.propagate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// propagate pushes parent selections to children.
func (r *Ranger) propagate() tea.Cmd {
	var cmds []tea.Cmd
	for i := 1; i < len(r.cols); i++ {
		if c := r.cols[i].SetParentSelection(r.cols[i-1].Selected()); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

// Focused — current focus index.
func (r *Ranger) Focused() int { return r.focus }

// SetFilter applies (or clears, when needle == "") a case-insensitive
// substring filter on the focused column. Selection in downstream
// columns is re-propagated so detail panes follow the new selection.
func (r *Ranger) SetFilter(needle string) tea.Cmd {
	r.cols[r.focus].SetFilter(needle)
	return r.propagate()
}

// GotoID jumps the focused column's selection to the row whose id
// matches needle (case-insensitive substring). Returns true on hit;
// on miss the selection is left unchanged. A hit propagates parent →
// child selection through the rest of the ranger.
func (r *Ranger) GotoID(needle string) (tea.Cmd, bool) {
	hit := r.cols[r.focus].GotoID(needle)
	if !hit {
		return nil, false
	}
	return r.propagate(), true
}

// FocusedSelection — id of the row currently highlighted in the
// focused column.
func (r *Ranger) FocusedSelection() string {
	return r.cols[r.focus].Selected()
}

// Stop releases background resources across all columns.
func (r *Ranger) Stop() {
	for _, c := range r.cols {
		c.Stop()
	}
}

// View renders the body at (width, height). Adaptive: at the
// narrowest widths we collapse to one column, then two, then the
// full layout.
func (r *Ranger) View(width, height int) string {
	switch {
	case width < 80:
		return r.renderCol(r.focus, width, height)
	case width < 100 && len(r.cols) >= 3:
		return r.renderPair(width, height)
	default:
		return r.renderFull(width, height)
	}
}

// renderFull lays out every column at its natural weight. The
// weights below tune the split for 3-col vs 2-col modes.
func (r *Ranger) renderFull(w, h int) string {
	switch len(r.cols) {
	case 2:
		gap := 1
		inner := w - gap
		// 40 / 60 — col 0 is a list, col 1 is the detail view
		// which wants room for kv lines and small JSON.
		l := inner * 40 / 100
		return joinHoriz(
			r.renderCol(0, l, h),
			r.renderCol(1, inner-l, h),
		)
	case 3:
		gaps := 2
		inner := w - gaps
		// 28 / 32 / 40 — see ranger commit for rationale.
		c0 := inner * 28 / 100
		c2 := inner * 40 / 100
		return joinHoriz(
			r.renderCol(0, c0, h),
			r.renderCol(1, inner-c0-c2, h),
			r.renderCol(2, c2, h),
		)
	default:
		return r.renderCol(r.focus, w, h)
	}
}

// renderPair (3-col only) shows the focused column + its child,
// or its parent at the right edge.
func (r *Ranger) renderPair(w, h int) string {
	leftIdx := r.focus
	rightIdx := r.focus + 1
	if rightIdx >= len(r.cols) {
		leftIdx = r.focus - 1
		rightIdx = r.focus
	}
	inner := w - 1
	cLw := inner * 40 / 100
	return joinHoriz(
		r.renderCol(leftIdx, cLw, h),
		r.renderCol(rightIdx, inner-cLw, h),
	)
}

// renderCol wraps one column in its border + title chip. Border
// brightens when active.
func (r *Ranger) renderCol(idx, w, h int) string {
	active := r.focus == idx
	border := theme.VioletMid
	titleStyle := theme.PaneTitle
	if active {
		border = theme.HorusBright
	} else {
		titleStyle = titleStyle.Foreground(theme.Dim)
	}

	// Total budget per column is w. The box adds 2 (border) outside
	// Width(), so Width(W) renders as W+2 visible cells. Padding(0,1)
	// then eats 2 more inside Width(), leaving W-2 for content. So
	// content gets w-4 cells; the box's Width() must be set to w-2.
	innerW := w - 4
	innerH := h - 4
	if innerW < 4 {
		innerW = 4
	}
	if innerH < 1 {
		innerH = 1
	}

	body := r.cols[idx].View(innerW, innerH, active)
	title := titleStyle.Render(r.cols[idx].Title())
	content := lipgloss.JoinVertical(lipgloss.Left, title, body)

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(innerW + 2).
		Height(innerH + 2).
		Render(content)
}

func joinHoriz(parts ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		interleave(parts, strings.Repeat(" ", 1))...)
}

func interleave(parts []string, sep string) []string {
	if len(parts) == 0 {
		return parts
	}
	out := make([]string, 0, 2*len(parts)-1)
	for i, p := range parts {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, p)
	}
	return out
}
