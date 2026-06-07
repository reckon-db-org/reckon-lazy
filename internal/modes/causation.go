package modes

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"codeberg.org/reckon-db-org/reckon-go/causation"
	"codeberg.org/reckon-db-org/reckon-go/streams"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ranger"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// eventNodeCol is one node in a causation walk: the event it
// represents plus a navigable list of its causal neighbours — its
// single cause (▲) and its direct effects (▼), from CausationService.
// It is a ranger.Brancher: selecting a neighbour and drilling (`l`)
// pushes a fresh eventNodeCol for that neighbour, so the breadcrumb
// traces the path through the causation graph. Walking is unbounded;
// the Drill pops (and Stop()s) nodes as you back out with `h`.
type eventNodeCol struct {
	api   *causation.Client
	event streams.RecordedEvent

	rows     []causRow // cause (if any) first, then effects, in order
	selected int
	loading  bool
	loaded   bool
	err      error
}

// causRow is one selectable neighbour of the node's event.
type causRow struct {
	effect bool // false = the cause (▲), true = an effect (▼)
	event  streams.RecordedEvent
}

func newEventNodeCol(api *causation.Client, ev streams.RecordedEvent) *eventNodeCol {
	return &eventNodeCol{api: api, event: ev, loading: true}
}

func (n *eventNodeCol) Title() string { return "causation" }

// Init fetches the event's cause and effects in one command.
func (n *eventNodeCol) Init() tea.Cmd {
	api := n.api
	id := n.event.EventID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		cause, cerr := api.Cause(ctx, id)
		effects, eerr := api.Effects(ctx, id)
		err := cerr
		if err == nil {
			err = eerr
		}
		return causLoadedMsg{id: id, cause: cause, effects: effects, err: err}
	}
}

type causLoadedMsg struct {
	id      string
	cause   streams.RecordedEvent
	effects []streams.RecordedEvent
	err     error
}

func (n *eventNodeCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(causLoadedMsg)
	if !ok {
		return nil, false
	}
	if m.id != n.event.EventID {
		return nil, true // for another node on the stack
	}
	n.loading, n.loaded, n.err = false, true, m.err
	n.rows = n.rows[:0]
	if m.cause.EventID != "" {
		n.rows = append(n.rows, causRow{effect: false, event: m.cause})
	}
	for _, ev := range m.effects {
		n.rows = append(n.rows, causRow{effect: true, event: ev})
	}
	n.selected = clamp(n.selected, 0, max(0, len(n.rows)-1))
	return nil, true
}

// SetParentSelection is a no-op: a node fetches by its own event id,
// fixed at construction, not from an upstream selection.
func (n *eventNodeCol) SetParentSelection(string) tea.Cmd { return nil }

func (n *eventNodeCol) Selected() string {
	if ev, ok := n.selectedEvent(); ok {
		return ev.EventID
	}
	return ""
}

func (n *eventNodeCol) selectedEvent() (streams.RecordedEvent, bool) {
	if n.selected < 0 || n.selected >= len(n.rows) {
		return streams.RecordedEvent{}, false
	}
	return n.rows[n.selected].event, true
}

func (n *eventNodeCol) Move(delta int) {
	if len(n.rows) == 0 {
		return
	}
	n.selected = clamp(n.selected+delta, 0, len(n.rows)-1)
}

func (n *eventNodeCol) SetFilter(string)   {}
func (n *eventNodeCol) GotoID(string) bool { return false }
func (n *eventNodeCol) Stop()              {}

// Crumb labels this node's event in the breadcrumb path.
func (n *eventNodeCol) Crumb() string {
	return fmt.Sprintf("%s v%d", truncate(n.event.StreamID, 16), n.event.Version)
}

// Child (ranger.Brancher) pushes a node for the selected neighbour.
func (n *eventNodeCol) Child() (ranger.Column, bool) {
	if ev, ok := n.selectedEvent(); ok {
		return newEventNodeCol(n.api, ev), true
	}
	return nil, false
}

func (n *eventNodeCol) View(w, h int, active bool) string {
	switch {
	case n.loading:
		return emptyHint("loading causation…")
	case n.err != nil:
		return errLine(n.err)
	}

	var b strings.Builder
	b.WriteString(kvLine("event", causEventLabel(n.event)) + "\n")
	if len(n.event.Tags) > 0 {
		b.WriteString(kvLine("tags", strings.Join(n.event.Tags, ", ")) + "\n")
	}
	b.WriteString("\n")

	row := 0
	emit := func(marker, label string) {
		sel := row == n.selected
		text := marker + " " + label
		switch {
		case sel && active:
			b.WriteString(theme.BadgeOK.Render("▸ ") + theme.RowKey.Render(text))
		case sel:
			b.WriteString(theme.RowDim.Render("▸ ") + theme.RowValue.Render(text))
		default:
			b.WriteString("  " + theme.RowValue.Render(text))
		}
		b.WriteString("\n")
		row++
	}

	hasCause := len(n.rows) > 0 && !n.rows[0].effect
	b.WriteString(theme.RowHeader.Render("▲ cause") + "\n")
	if hasCause {
		emit("▲", causEventLabel(n.rows[0].event))
	} else {
		b.WriteString(theme.RowDim.Render("  (no recorded cause)") + "\n")
	}

	effects := n.rows
	if hasCause {
		effects = n.rows[1:]
	}
	b.WriteString("\n" + theme.RowHeader.Render(fmt.Sprintf("▼ effects (%d)", len(effects))) + "\n")
	if len(effects) == 0 {
		b.WriteString(theme.RowDim.Render("  (no effects)"))
	} else {
		for _, r := range effects {
			emit("▼", causEventLabel(r.event))
		}
	}
	return clip(b.String(), h)
}

func causEventLabel(ev streams.RecordedEvent) string {
	return fmt.Sprintf("%s v%d  %s", truncate(ev.StreamID, 24), ev.Version, ev.EventType)
}
