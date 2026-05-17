package modes

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/streams"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ranger"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// StreamsView is the wired ranger triple for the streams mode plus
// typed handles to the columns that need side-channel updates from
// the parent model (e.g. binding the selected event into the detail
// column).
type StreamsView struct {
	Ranger      *ranger.Ranger
	streamsCol  *streamListCol
	eventsCol   *eventListCol
	detailCol   *eventDetailCol
}

// BuildStreams returns a wired StreamsView bound to (client, store).
func BuildStreams(c *reckon.Client, store string) *StreamsView {
	api := c.Streams(store)
	streamsCol := newStreamListCol(api)
	eventsCol := newEventListCol(api)
	detailCol := newEventDetailCol()
	return &StreamsView{
		Ranger:     ranger.New(streamsCol, eventsCol, detailCol),
		streamsCol: streamsCol,
		eventsCol:  eventsCol,
		detailCol:  detailCol,
	}
}

// SyncDetail copies the currently-selected event from column 2 into
// column 3. Call once per top-level Update so the detail view reflects
// the latest selection without needing inter-column reach-around.
func (v *StreamsView) SyncDetail() {
	if ev, ok := v.eventsCol.selectedEvent(); ok {
		v.detailCol.set(&ev)
	} else {
		v.detailCol.set(nil)
	}
}

// SelectedEvent — currently-highlighted event in column 2, if any.
// Used by the `e` editor handoff in the parent model.
func (v *StreamsView) SelectedEvent() (streams.RecordedEvent, bool) {
	return v.eventsCol.selectedEvent()
}

//------------------------------------------------------------------------------
// Column 1 — stream id list

type streamListCol struct {
	api      *streams.Client
	items    []string
	selected int
	err      error
	loading  bool
}

func newStreamListCol(api *streams.Client) *streamListCol {
	return &streamListCol{api: api, loading: true}
}

func (s *streamListCol) Title() string { return "streams" }

func (s *streamListCol) Init() tea.Cmd { return s.fetch() }

func (s *streamListCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(streamListLoadedMsg); ok {
		s.items, s.err, s.loading = m.items, m.err, false
		s.selected = clamp(s.selected, 0, max(0, len(s.items)-1))
		return nil, true
	}
	return nil, false
}

func (s *streamListCol) SetParentSelection(string) tea.Cmd { return nil }

func (s *streamListCol) Selected() string {
	if s.selected < 0 || s.selected >= len(s.items) {
		return ""
	}
	return s.items[s.selected]
}

func (s *streamListCol) Move(delta int) {
	if len(s.items) == 0 {
		return
	}
	s.selected = clamp(s.selected+delta, 0, len(s.items)-1)
}

func (s *streamListCol) View(w, h int, active bool) string {
	switch {
	case s.loading:
		return emptyHint("loading…")
	case s.err != nil:
		return errLine(s.err)
	case len(s.items) == 0:
		return emptyHint("no streams yet")
	}
	return renderList(s.items, s.selected, w, h, active)
}

func (s *streamListCol) Stop() {}

func (s *streamListCol) fetch() tea.Cmd {
	api := s.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		ids, err := api.List(ctx)
		if err == nil {
			sort.Strings(ids)
		}
		return streamListLoadedMsg{items: ids, err: err}
	}
}

type streamListLoadedMsg struct {
	items []string
	err   error
}

//------------------------------------------------------------------------------
// Column 2 — events in the selected stream

type eventListCol struct {
	api      *streams.Client
	parent   string
	events   []streams.RecordedEvent
	selected int
	loading  bool
	err      error
}

func newEventListCol(api *streams.Client) *eventListCol {
	return &eventListCol{api: api}
}

func (e *eventListCol) Title() string {
	if e.parent == "" {
		return "events"
	}
	return "events · " + truncate(e.parent, 22)
}

func (e *eventListCol) Init() tea.Cmd { return nil }

func (e *eventListCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(eventListLoadedMsg); ok {
		if m.streamID != e.parent {
			return nil, true // stale (parent changed while RPC in flight)
		}
		e.events, e.err, e.loading = m.events, m.err, false
		e.selected = clamp(e.selected, 0, max(0, len(e.events)-1))
		return nil, true
	}
	return nil, false
}

func (e *eventListCol) SetParentSelection(parent string) tea.Cmd {
	if parent == e.parent {
		return nil
	}
	e.parent = parent
	e.events = nil
	e.selected = 0
	e.err = nil
	if parent == "" {
		e.loading = false
		return nil
	}
	e.loading = true
	api := e.api
	stream := parent
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		evs, err := api.Read(ctx, stream, 0, 500)
		return eventListLoadedMsg{streamID: stream, events: evs, err: err}
	}
}

func (e *eventListCol) Selected() string {
	if ev, ok := e.selectedEvent(); ok {
		return fmt.Sprintf("%s/%d", e.parent, ev.Version)
	}
	return ""
}

func (e *eventListCol) selectedEvent() (streams.RecordedEvent, bool) {
	if e.selected < 0 || e.selected >= len(e.events) {
		return streams.RecordedEvent{}, false
	}
	return e.events[e.selected], true
}

func (e *eventListCol) Move(delta int) {
	if len(e.events) == 0 {
		return
	}
	e.selected = clamp(e.selected+delta, 0, len(e.events)-1)
}

func (e *eventListCol) View(w, h int, active bool) string {
	switch {
	case e.parent == "":
		return emptyHint("select a stream →")
	case e.loading:
		return emptyHint("loading…")
	case e.err != nil:
		return errLine(e.err)
	case len(e.events) == 0:
		return emptyHint("(empty stream)")
	}
	labels := make([]string, len(e.events))
	for i, ev := range e.events {
		labels[i] = fmt.Sprintf("v%-4d %s", ev.Version, truncate(ev.EventType, w-8))
	}
	return renderList(labels, e.selected, w, h, active)
}

func (e *eventListCol) Stop() {}

type eventListLoadedMsg struct {
	streamID string
	events   []streams.RecordedEvent
	err      error
}

//------------------------------------------------------------------------------
// Column 3 — event detail

type eventDetailCol struct {
	source *streams.RecordedEvent
}

func newEventDetailCol() *eventDetailCol                          { return &eventDetailCol{} }
func (e *eventDetailCol) Title() string                           { return "detail" }
func (e *eventDetailCol) Init() tea.Cmd                           { return nil }
func (e *eventDetailCol) Update(tea.Msg) (tea.Cmd, bool)          { return nil, false }
func (e *eventDetailCol) SetParentSelection(string) tea.Cmd       { return nil }
func (e *eventDetailCol) Move(int)                                {}
func (e *eventDetailCol) Stop()                                   {}
func (e *eventDetailCol) set(ev *streams.RecordedEvent)           { e.source = ev }
func (e *eventDetailCol) Selected() string {
	if e.source == nil {
		return ""
	}
	return e.source.EventID
}

func (e *eventDetailCol) View(w, h int, active bool) string {
	if e.source == nil {
		return emptyHint("select an event →")
	}
	ev := e.source
	var b strings.Builder
	b.WriteString(kvLine("type", ev.EventType) + "\n")
	b.WriteString(kvLine("version", fmt.Sprintf("%d", ev.Version)) + "\n")
	b.WriteString(kvLine("id", ev.EventID) + "\n")
	b.WriteString(kvLine("stream", ev.StreamID) + "\n")
	b.WriteString(kvLine("when", ev.Timestamp.Format("2006-01-02 15:04:05")) + "\n")
	if len(ev.Tags) > 0 {
		b.WriteString(kvLine("tags", strings.Join(ev.Tags, ", ")) + "\n")
	}
	if ev.DataContentType != "" {
		b.WriteString(kvLine("content", ev.DataContentType) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(theme.RowHeader.Render("data") + "\n")
	b.WriteString(theme.RowValue.Render(prettyJSON(ev.Data, w)))
	if len(ev.Metadata) > 0 {
		b.WriteString("\n\n" + theme.RowHeader.Render("metadata") + "\n")
		b.WriteString(theme.RowDim.Render(prettyJSON(ev.Metadata, w)))
	}
	return clip(b.String(), h)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
