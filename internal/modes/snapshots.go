package modes

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/snapshots"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ranger"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// SnapshotsView — 3-pane ranger:
//
//   Col 0: streams that have snapshots
//   Col 1: snapshot versions for the selected stream
//   Col 2: snapshot data + anchor hash + metadata
//
// The gateway/db ignores source_uuid in storage (the handler
// passes it through but reckon_db_snapshots:load_at/3 only takes
// stream_uuid + version), so we pass "" for source throughout.
// If a future deployment uses source as a real namespace we'll
// add a config knob or a heuristic.
type SnapshotsView struct {
	Ranger      *ranger.Ranger
	streamsCol  *snapStreamsCol
	versionsCol *snapVersionsCol
	dataCol     *snapDataCol
}

func BuildSnapshots(c *reckon.Client, store string) *SnapshotsView {
	api := c.Snapshots(store)
	streamsCol := newSnapStreamsCol(api)
	versionsCol := newSnapVersionsCol(api)
	dataCol := newSnapDataCol()
	return &SnapshotsView{
		Ranger:      ranger.New(streamsCol, versionsCol, dataCol),
		streamsCol:  streamsCol,
		versionsCol: versionsCol,
		dataCol:     dataCol,
	}
}

// SyncDetail mirrors col 1's selected snapshot into col 2.
func (v *SnapshotsView) SyncDetail() {
	if rec, ok := v.versionsCol.selectedRecord(); ok {
		v.dataCol.set(&rec)
	} else {
		v.dataCol.set(nil)
	}
}

// SelectedSnapshot — record currently highlighted in col 1. Used
// by main for the `e' editor handoff.
func (v *SnapshotsView) SelectedSnapshot() (snapshots.Record, bool) {
	return v.versionsCol.selectedRecord()
}

// Refresh re-fetches the stream list (col 0). Versions (col 1)
// re-fetch automatically when the selected stream changes; we
// don't refetch them here without a selection-change signal.
// Bound to `r' in the parent model.
func (v *SnapshotsView) Refresh() tea.Cmd {
	v.streamsCol.loading = true
	return v.streamsCol.fetch()
}

//------------------------------------------------------------------------------
// Col 0 — streams that have snapshots

type snapStreamsCol struct {
	api      *snapshots.Client
	items    []string
	selected int
	err      error
	loading  bool
	filter   string
	visible  []int
}

func newSnapStreamsCol(api *snapshots.Client) *snapStreamsCol {
	return &snapStreamsCol{api: api, loading: true}
}

func (s *snapStreamsCol) Title() string                     { return "streams" }
func (s *snapStreamsCol) Init() tea.Cmd                     { return s.fetch() }
func (s *snapStreamsCol) SetParentSelection(string) tea.Cmd { return nil }
func (s *snapStreamsCol) Stop()                             {}

func (s *snapStreamsCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(snapStreamsLoadedMsg); ok {
		s.items, s.err, s.loading = m.items, m.err, false
		s.selected = clamp(s.selected, 0, max(0, len(s.items)-1))
		return nil, true
	}
	return nil, false
}

func (s *snapStreamsCol) Selected() string {
	idx := s.visibleSelected()
	if idx < 0 {
		return ""
	}
	return s.items[idx]
}

func (s *snapStreamsCol) visibleSelected() int {
	if s.filter == "" {
		if s.selected < 0 || s.selected >= len(s.items) {
			return -1
		}
		return s.selected
	}
	if s.selected < 0 || s.selected >= len(s.visible) {
		return -1
	}
	return s.visible[s.selected]
}

func (s *snapStreamsCol) Move(delta int) {
	n := len(s.items)
	if s.filter != "" {
		n = len(s.visible)
	}
	if n == 0 {
		return
	}
	s.selected = clamp(s.selected+delta, 0, n-1)
}

func (s *snapStreamsCol) SetFilter(needle string) {
	s.filter = needle
	s.visible = filterIndices(s.items, needle)
	n := len(s.items)
	if s.filter != "" {
		n = len(s.visible)
	}
	if n == 0 {
		s.selected = 0
		return
	}
	s.selected = clamp(s.selected, 0, n-1)
}

func (s *snapStreamsCol) GotoID(needle string) bool {
	idx := findIndex(s.items, needle)
	if idx < 0 {
		return false
	}
	s.filter = ""
	s.visible = nil
	s.selected = idx
	return true
}

func (s *snapStreamsCol) View(w, h int, active bool) string {
	switch {
	case s.loading:
		return emptyHint("loading…")
	case s.err != nil:
		return errLine(s.err)
	case len(s.items) == 0:
		return emptyHint("no snapshots yet")
	}
	if s.filter != "" {
		rows := make([]string, 0, len(s.visible))
		for _, i := range s.visible {
			rows = append(rows, s.items[i])
		}
		if len(rows) == 0 {
			return emptyHint("no match")
		}
		return renderList(rows, s.selected, w, h, active)
	}
	return renderList(s.items, s.selected, w, h, active)
}

func (s *snapStreamsCol) fetch() tea.Cmd {
	api := s.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		all, err := api.ListAll(ctx)
		if err != nil {
			return snapStreamsLoadedMsg{err: err}
		}
		seen := map[string]struct{}{}
		for _, r := range all {
			seen[r.StreamID] = struct{}{}
		}
		out := make([]string, 0, len(seen))
		for id := range seen {
			out = append(out, id)
		}
		sort.Strings(out)
		return snapStreamsLoadedMsg{items: out}
	}
}

type snapStreamsLoadedMsg struct {
	items []string
	err   error
}

//------------------------------------------------------------------------------
// Col 1 — versions for selected stream

type snapVersionsCol struct {
	api      *snapshots.Client
	parent   string
	items    []snapshots.Record
	selected int
	err      error
	loading  bool
}

func newSnapVersionsCol(api *snapshots.Client) *snapVersionsCol {
	return &snapVersionsCol{api: api}
}

func (v *snapVersionsCol) Title() string {
	if v.parent == "" {
		return "versions"
	}
	return "versions · " + truncate(v.parent, 20)
}

func (v *snapVersionsCol) Init() tea.Cmd { return nil }

func (v *snapVersionsCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(snapVersionsLoadedMsg); ok {
		if m.streamID != v.parent {
			return nil, true
		}
		v.items, v.err, v.loading = m.items, m.err, false
		// Snapshots are typically appended monotonically; show
		// newest first so the latest checkpoint is what you see
		// without scrolling.
		sort.SliceStable(v.items, func(i, j int) bool {
			return v.items[i].Version > v.items[j].Version
		})
		v.selected = clamp(v.selected, 0, max(0, len(v.items)-1))
		return nil, true
	}
	return nil, false
}

func (v *snapVersionsCol) SetParentSelection(parent string) tea.Cmd {
	if parent == v.parent {
		return nil
	}
	v.parent = parent
	v.items = nil
	v.selected = 0
	v.err = nil
	if parent == "" {
		v.loading = false
		return nil
	}
	v.loading = true
	api := v.api
	stream := parent
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		list, err := api.List(ctx, "", stream)
		return snapVersionsLoadedMsg{streamID: stream, items: list, err: err}
	}
}

func (v *snapVersionsCol) Selected() string {
	if rec, ok := v.selectedRecord(); ok {
		return fmt.Sprintf("%s@%d", v.parent, rec.Version)
	}
	return ""
}

func (v *snapVersionsCol) selectedRecord() (snapshots.Record, bool) {
	if v.selected < 0 || v.selected >= len(v.items) {
		return snapshots.Record{}, false
	}
	return v.items[v.selected], true
}

func (v *snapVersionsCol) Move(delta int) {
	if len(v.items) == 0 {
		return
	}
	v.selected = clamp(v.selected+delta, 0, len(v.items)-1)
}

// Filter / Goto on a small ordered version list aren't terribly
// useful (the list is monotonic), so accept the calls and search by
// version number only.
func (v *snapVersionsCol) SetFilter(string) {}

func (v *snapVersionsCol) GotoID(needle string) bool {
	needle = strings.TrimPrefix(needle, "v")
	for i, r := range v.items {
		if fmt.Sprintf("%d", r.Version) == needle {
			v.selected = i
			return true
		}
	}
	return false
}

func (v *snapVersionsCol) View(w, h int, active bool) string {
	switch {
	case v.parent == "":
		return emptyHint("select a stream →")
	case v.loading:
		return emptyHint("loading…")
	case v.err != nil:
		return errLine(v.err)
	case len(v.items) == 0:
		return emptyHint("(no snapshots)")
	}
	labels := make([]string, len(v.items))
	for i, r := range v.items {
		labels[i] = fmt.Sprintf("v%-6d %s",
			r.Version,
			theme.RowDim.Inline(true).Render(humanAgo(r.Timestamp)),
		)
	}
	return renderList(labels, v.selected, w, h, active)
}

func (v *snapVersionsCol) Stop() {}

type snapVersionsLoadedMsg struct {
	streamID string
	items    []snapshots.Record
	err      error
}

//------------------------------------------------------------------------------
// Col 2 — snapshot data + anchor hash + metadata

type snapDataCol struct {
	rec *snapshots.Record
}

func newSnapDataCol() *snapDataCol                            { return &snapDataCol{} }
func (d *snapDataCol) Title() string                          { return "data" }
func (d *snapDataCol) Init() tea.Cmd                          { return nil }
func (d *snapDataCol) Update(tea.Msg) (tea.Cmd, bool)         { return nil, false }
func (d *snapDataCol) SetParentSelection(string) tea.Cmd      { return nil }
func (d *snapDataCol) SetFilter(string)                       {}
func (d *snapDataCol) GotoID(string) bool                     { return false }
func (d *snapDataCol) Selected() string {
	if d.rec == nil {
		return ""
	}
	return fmt.Sprintf("%s@%d", d.rec.StreamID, d.rec.Version)
}
func (d *snapDataCol) Move(int)                       {}
func (d *snapDataCol) Stop()                          {}
func (d *snapDataCol) set(r *snapshots.Record)        { d.rec = r }

func (d *snapDataCol) View(w, h int, active bool) string {
	if d.rec == nil {
		return emptyHint("—")
	}
	r := d.rec
	var b strings.Builder
	b.WriteString(kvLine("stream", r.StreamID) + "\n")
	b.WriteString(kvLine("version", fmt.Sprintf("%d", r.Version)) + "\n")
	if !r.Timestamp.IsZero() {
		b.WriteString(kvLine("when", r.Timestamp.Format("2006-01-02 15:04:05")) + "\n")
	}
	if len(r.AnchorHash) > 0 {
		b.WriteString(kvLine("anchor", fmt.Sprintf("%x", r.AnchorHash[:min(8, len(r.AnchorHash))])+"…") + "\n")
	}
	b.WriteString("\n" + theme.RowHeader.Render("data") + "\n")
	b.WriteString(theme.RowValue.Render(prettyJSON(r.Data, w)))
	if len(r.Metadata) > 0 {
		b.WriteString("\n\n" + theme.RowHeader.Render("metadata") + "\n")
		b.WriteString(theme.RowDim.Render(prettyJSON(r.Metadata, w)))
	}
	return clip(b.String(), h)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
