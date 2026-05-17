package ranger

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// Ranger holds the three columns and drives the miller-columns
// layout, focus, and parent→child selection propagation.
//
// Construction order matters: cols[0] feeds cols[1] feeds cols[2].
// Cols 1 and 2 read their parent's Selected() value through
// SetParentSelection — typically driven from Update once focus or
// selection changes.
type Ranger struct {
	cols  [3]Column
	focus int // 0, 1, or 2
}

// New builds a Ranger from three columns. cols[0] is leftmost.
func New(left, middle, right Column) *Ranger {
	return &Ranger{cols: [3]Column{left, middle, right}, focus: 0}
}

// Init fans Init() out to all columns.
func (r *Ranger) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, 3)
	for _, c := range r.cols {
		if cmd := c.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// HandleKey processes a navigation key. Returns the cmd to chain (or
// nil) and whether the key was consumed.
func (r *Ranger) HandleKey(key string) (tea.Cmd, bool) {
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
		if r.focus < 2 {
			r.focus++
		}
		return nil, true
	case "g", "home":
		// Jump to top — implemented as a large negative move so
		// columns don't need a separate API.
		r.cols[r.focus].Move(-1 << 30)
		return r.propagate(), true
	case "G", "end":
		r.cols[r.focus].Move(1 << 30)
		return r.propagate(), true
	}
	return nil, false
}

// Update routes a tea.Msg through every column (so background streams
// in non-active columns keep draining). Returns the batched cmds.
func (r *Ranger) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range r.cols {
		if cmd, _ := c.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Re-propagate after each batch — events that changed the
	// upstream column's contents may have shifted selection.
	if cmd := r.propagate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// propagate pushes parent selections to children. Returns the batch
// of cmds from any column that wants to refetch on parent change.
func (r *Ranger) propagate() tea.Cmd {
	var cmds []tea.Cmd
	if c := r.cols[1].SetParentSelection(r.cols[0].Selected()); c != nil {
		cmds = append(cmds, c)
	}
	if c := r.cols[2].SetParentSelection(r.cols[1].Selected()); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// Focused — current focus index (0/1/2).
func (r *Ranger) Focused() int { return r.focus }

// FocusedSelection — id of the row currently highlighted in the
// focused column. Useful for actions like `e` (edit) that act on
// "whatever is selected right now."
func (r *Ranger) FocusedSelection() string {
	return r.cols[r.focus].Selected()
}

// Stop releases background resources across all columns.
func (r *Ranger) Stop() {
	for _, c := range r.cols {
		c.Stop()
	}
}

// View renders the three-column body at the given outer (width, height).
// Adaptive: 3 cols ≥ 100w, 2 cols 80-99w (collapses the parent of the
// focused col), 1 col < 80w (only the focused col).
func (r *Ranger) View(width, height int) string {
	if width < 80 {
		return r.renderOne(width, height)
	}
	if width < 100 {
		return r.renderTwo(width, height)
	}
	return r.renderThree(width, height)
}

func (r *Ranger) renderThree(w, h int) string {
	// 1 char gap between columns
	gaps := 2
	inner := w - gaps
	// Heavier weight on cols 1+2 (the working columns); col 0
	// (the list) tends to be narrower.
	c0w := inner * 22 / 100
	c2w := inner * 38 / 100
	c1w := inner - c0w - c2w
	return joinHoriz(
		r.renderCol(0, c0w, h),
		r.renderCol(1, c1w, h),
		r.renderCol(2, c2w, h),
	)
}

func (r *Ranger) renderTwo(w, h int) string {
	// Show the focused column + its child (or its parent, if focus
	// is on column 2 with no child).
	leftIdx := r.focus
	rightIdx := r.focus + 1
	if rightIdx > 2 {
		leftIdx = r.focus - 1
		rightIdx = r.focus
	}
	inner := w - 1
	cLw := inner * 40 / 100
	cRw := inner - cLw
	return joinHoriz(
		r.renderCol(leftIdx, cLw, h),
		r.renderCol(rightIdx, cRw, h),
	)
}

func (r *Ranger) renderOne(w, h int) string {
	return r.renderCol(r.focus, w, h)
}

// renderCol wraps one column in its border + title chip. Border color
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

	// inner content area inside the border
	innerW := w - 2 // border
	innerH := h - 4 // border + title + breadcrumb spacer
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
