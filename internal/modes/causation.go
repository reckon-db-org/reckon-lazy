package modes

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc/status"

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

	// cause and effects are resolved independently — one failing or
	// timing out must not blank the other (the gateway collapses
	// "no cause", timeout, and node-down all into NOT_FOUND, so a
	// degraded backend would otherwise wipe the whole node).
	cause      *streams.RecordedEvent
	causeErr   error
	effects    []streams.RecordedEvent
	effectsErr error

	rows     []causRow // selectable neighbours: cause (if any) then effects
	selected int
	loading  bool
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

// Init fetches the event's cause and effects concurrently, so a slow
// or hung side can't starve the other of the shared deadline.
func (n *eventNodeCol) Init() tea.Cmd {
	api := n.api
	id := n.event.EventID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		type cres struct {
			ev  streams.RecordedEvent
			err error
		}
		type eres struct {
			evs []streams.RecordedEvent
			err error
		}
		cch, ech := make(chan cres, 1), make(chan eres, 1)
		go func() { ev, err := api.Cause(ctx, id); cch <- cres{ev, err} }()
		go func() { evs, err := api.Effects(ctx, id); ech <- eres{evs, err} }()
		c, e := <-cch, <-ech
		return causLoadedMsg{
			id:    id,
			cause: c.ev, causeErr: c.err,
			effects: e.evs, effectsErr: e.err,
		}
	}
}

type causLoadedMsg struct {
	id         string
	cause      streams.RecordedEvent
	causeErr   error
	effects    []streams.RecordedEvent
	effectsErr error
}

func (n *eventNodeCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(causLoadedMsg)
	if !ok {
		return nil, false
	}
	if m.id != n.event.EventID {
		return nil, true // for another node on the stack
	}
	n.loading = false
	n.causeErr, n.effectsErr = m.causeErr, m.effectsErr
	n.cause = nil
	if m.causeErr == nil && m.cause.EventID != "" {
		ev := m.cause
		n.cause = &ev
	}
	n.effects = m.effects

	n.rows = n.rows[:0]
	if n.cause != nil {
		n.rows = append(n.rows, causRow{effect: false, event: *n.cause})
	}
	for _, ev := range n.effects {
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
	if n.loading {
		return emptyHint("loading causation…")
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

	// Cause section — resolved independently of effects.
	b.WriteString(theme.RowHeader.Render("▲ cause") + "\n")
	switch {
	case n.causeErr != nil:
		// The gateway can't distinguish "no cause" from a transient
		// failure (both are NOT_FOUND), so report it honestly.
		b.WriteString(theme.RowDim.Render("  (unavailable — "+causReason(n.causeErr)+")") + "\n")
	case n.cause == nil:
		b.WriteString(theme.RowDim.Render("  (no recorded cause)") + "\n")
	default:
		emit("▲", causEventLabel(*n.cause))
	}

	// Effects section — resolved independently of cause.
	b.WriteString("\n" + theme.RowHeader.Render(fmt.Sprintf("▼ effects (%d)", len(n.effects))) + "\n")
	switch {
	case n.effectsErr != nil:
		b.WriteString(theme.RowDim.Render("  (unavailable — " + causReason(n.effectsErr) + ")"))
	case len(n.effects) == 0:
		b.WriteString(theme.RowDim.Render("  (no effects)"))
	default:
		for _, ev := range n.effects {
			emit("▼", causEventLabel(ev))
		}
	}
	return clip(b.String(), h)
}

// causReason renders a gRPC error compactly (status code name when
// available, else the trimmed message).
func causReason(err error) string {
	if s, ok := status.FromError(err); ok {
		return strings.ToLower(s.Code().String())
	}
	return truncate(err.Error(), 24)
}

func causEventLabel(ev streams.RecordedEvent) string {
	return fmt.Sprintf("%s v%d  %s", truncate(ev.StreamID, 24), ev.Version, ev.EventType)
}
