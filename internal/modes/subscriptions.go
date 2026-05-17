package modes

import (
	tea "github.com/charmbracelet/bubbletea"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ranger"
)

// SubscriptionsView — placeholder mode until the columns are wired.
type SubscriptionsView struct {
	Ranger *ranger.Ranger
}

func BuildSubscriptions(_ *reckon.Client, _ string) *SubscriptionsView {
	left := stubCol{title: "subscriptions", hint: "will list SubscriptionService.ListSubscriptions"}
	mid := stubCol{title: "events behind", hint: "live lag + checkpoint position"}
	right := stubCol{title: "detail", hint: "subscription metadata + ack rate"}
	return &SubscriptionsView{Ranger: ranger.New(&left, &mid, &right)}
}

func (v *SubscriptionsView) SyncDetail() {}

// SnapshotsView — placeholder mode.
type SnapshotsView struct {
	Ranger *ranger.Ranger
}

func BuildSnapshots(_ *reckon.Client, _ string) *SnapshotsView {
	left := stubCol{title: "streams", hint: "streams that have snapshots"}
	mid := stubCol{title: "versions", hint: "snapshot versions for the selected stream"}
	right := stubCol{title: "data", hint: "snapshot payload + anchor hash"}
	return &SnapshotsView{Ranger: ranger.New(&left, &mid, &right)}
}

func (v *SnapshotsView) SyncDetail() {}

//------------------------------------------------------------------------------
// stubCol — a column that does nothing but render its hint. Shared by
// the unwired modes.

type stubCol struct {
	title string
	hint  string
}

func (c *stubCol) Title() string                          { return c.title }
func (c *stubCol) Init() tea.Cmd                          { return nil }
func (c *stubCol) Update(tea.Msg) (tea.Cmd, bool)         { return nil, false }
func (c *stubCol) SetParentSelection(string) tea.Cmd      { return nil }
func (c *stubCol) Selected() string                       { return "" }
func (c *stubCol) Move(int)                               {}
func (c *stubCol) Stop()                                  {}
func (c *stubCol) View(w, h int, active bool) string      { return emptyHint(c.hint) }
