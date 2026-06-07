package ranger

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fakeCol is a well-behaved Column: like the real columns it clips its
// View to the (w, h) it's handed. The budget test then guards Drill's
// composition — that it passes the right height to both panes and the
// rail+gap+preview widths sum to the frame — not the per-column clip
// (which renderColumn delegates and the stores test already covers).
type fakeCol struct {
	title  string
	sel    string
	parent string
}

func (f *fakeCol) Title() string                  { return f.title }
func (f *fakeCol) Init() tea.Cmd                  { return nil }
func (f *fakeCol) Update(tea.Msg) (tea.Cmd, bool) { return nil, false }
func (f *fakeCol) SetParentSelection(p string) tea.Cmd {
	f.parent = p
	return nil
}
func (f *fakeCol) Selected() string   { return f.sel }
func (f *fakeCol) Crumb() string      { return f.sel } // exercises the Crumber path
func (f *fakeCol) Move(int)           {}
func (f *fakeCol) SetFilter(string)   {}
func (f *fakeCol) GotoID(string) bool { return false }
func (f *fakeCol) Stop()              {}
func (f *fakeCol) View(w, h int, _ bool) string {
	if h < 1 {
		h = 1
	}
	if w < 1 {
		w = 1
	}
	// A line wider than any pane, clipped to w — proves the rail/preview
	// width budget holds even under over-long content.
	line := strings.Repeat("x", 300)
	if len(line) > w {
		line = line[:w]
	}
	rows := make([]string, h)
	for i := range rows {
		rows[i] = line
	}
	return strings.Join(rows, "\n")
}

// TestDrillViewFitsBudget guards the same doubled/stacked-frame bug the
// stores test guards, for the drill layout: at every drill depth the
// body MUST render exactly the height it's handed and no row may
// exceed the width — otherwise a wrapped line scrolls the status bar
// into a stale duplicate.
func TestDrillViewFitsBudget(t *testing.T) {
	geoms := []struct{ w, h int }{
		{187, 44}, {120, 30}, {100, 24}, {80, 20}, {60, 16}, {200, 50},
	}
	for _, focus := range []int{0, 1, 2} { // root list, mid, leaf (zoom)
		for _, g := range geoms {
			d := NewDrill(
				&fakeCol{title: "streams"},
				&fakeCol{title: "events"},
				&fakeCol{title: "detail"},
			)
			d.focus = focus

			out := d.View(g.w, g.h)
			lines := strings.Split(out, "\n")
			if len(lines) != g.h {
				t.Errorf("focus=%d w=%d h=%d: rendered %d lines, want exactly %d",
					focus, g.w, g.h, len(lines), g.h)
			}
			for i, ln := range lines {
				if got := lipgloss.Width(ln); got > g.w {
					t.Errorf("focus=%d w=%d h=%d: row %d width %d exceeds %d",
						focus, g.w, g.h, i, got, g.w)
				}
			}
		}
	}
}

// branchCol is a fakeCol that can branch into child nodes (ranger.Brancher)
// and records Stop() so the pop test can assert the popped node is released.
type branchCol struct {
	fakeCol
	stopped *bool
}

func (b *branchCol) Child() (Column, bool) {
	return &branchCol{fakeCol: fakeCol{title: "node", sel: "n"}, stopped: b.stopped}, true
}
func (b *branchCol) Stop() {
	if b.stopped != nil {
		*b.stopped = true
	}
}

// TestDrillBranchPushPop checks the dynamic stack: drilling past a
// Brancher leaf pushes a node, the breadcrumb grows, and `h` pops the
// node and Stop()s it while never popping a base-chain column.
func TestDrillBranchPushPop(t *testing.T) {
	stopped := false
	leaf := &branchCol{fakeCol: fakeCol{title: "leaf", sel: "e"}, stopped: &stopped}
	d := NewDrill(&fakeCol{title: "root", sel: "r"}, leaf)
	if d.base != 2 {
		t.Fatalf("base = %d, want 2", d.base)
	}

	d.HandleKey("l") // root -> leaf (focus 1, the last base col)
	if d.focus != 1 {
		t.Fatalf("focus = %d, want 1", d.focus)
	}

	d.HandleKey("l") // branch: push a node
	if d.focus != 2 || len(d.cols) != 3 {
		t.Fatalf("after branch: focus=%d cols=%d, want 2/3", d.focus, len(d.cols))
	}
	if got := d.Crumbs(); len(got) != 3 {
		t.Fatalf("crumbs = %v, want length 3 (root, leaf, node)", got)
	}

	d.HandleKey("h") // pop the node
	if d.focus != 1 || len(d.cols) != 2 {
		t.Fatalf("after pop: focus=%d cols=%d, want 1/2", d.focus, len(d.cols))
	}
	if !stopped {
		t.Error("popped node was not Stop()ed")
	}

	d.HandleKey("h") // back into the base chain — must not truncate
	if d.focus != 0 || len(d.cols) != 2 {
		t.Fatalf("h in base chain: focus=%d cols=%d, want 0/2 (no truncation)", d.focus, len(d.cols))
	}
}

// TestDrillCrumbsAndFocus checks the breadcrumb grows as you drill in
// and that l/h move the focus (and propagate parent selections).
func TestDrillCrumbsAndFocus(t *testing.T) {
	a := &fakeCol{title: "streams", sel: "orders-42"}
	b := &fakeCol{title: "events", sel: "v7 lot_occupied"}
	leaf := &fakeCol{title: "detail"} // empty Crumb → skipped
	d := NewDrill(a, b, leaf)

	if got := d.Crumbs(); len(got) != 1 || got[0] != "orders-42" {
		t.Fatalf("focus 0 crumbs = %v, want [orders-42]", got)
	}

	// Drill in once.
	if _, handled := d.HandleKey("l"); !handled {
		t.Fatal("l not handled")
	}
	if d.focus != 1 {
		t.Fatalf("focus = %d, want 1 after l", d.focus)
	}
	if got := b.parent; got != "orders-42" {
		t.Fatalf("child parent = %q, want orders-42 (propagate)", got)
	}
	if got := d.Crumbs(); len(got) != 2 || got[1] != "v7 lot_occupied" {
		t.Fatalf("focus 1 crumbs = %v, want [orders-42, v7 lot_occupied]", got)
	}

	// Drill to the leaf — empty crumb is skipped, path stays length 2.
	d.HandleKey("l")
	if d.focus != 2 {
		t.Fatalf("focus = %d, want 2", d.focus)
	}
	if got := d.Crumbs(); len(got) != 2 {
		t.Fatalf("leaf crumbs = %v, want length 2 (leaf skipped)", got)
	}

	// Drill back out.
	d.HandleKey("h")
	if d.focus != 1 {
		t.Fatalf("focus = %d, want 1 after h", d.focus)
	}
}
