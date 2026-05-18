package modes

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/subscriptions"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ranger"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// SubscriptionsView — 3-pane ranger:
//
//   Col 0: subscription list (name + type + checkpoint chip)
//   Col 1: lag detail for the selected subscription
//   Col 2: full info (id, type, selector, name, pool, created_at)
//
// Data comes from SubscriptionService.List + GetSubscriptionLag.
// The list refreshes on Init + every 5s; lag refetches whenever
// the selected sub changes.
type SubscriptionsView struct {
	Ranger  *ranger.Ranger
	listCol *subListCol
	lagCol  *subLagCol
	infoCol *subInfoCol
}

func BuildSubscriptions(c *reckon.Client, store string) *SubscriptionsView {
	api := c.Subscriptions(store)
	listCol := newSubListCol(api)
	lagCol := newSubLagCol(api)
	infoCol := newSubInfoCol()
	return &SubscriptionsView{
		Ranger:  ranger.New(listCol, lagCol, infoCol),
		listCol: listCol,
		lagCol:  lagCol,
		infoCol: infoCol,
	}
}

// SyncDetail wires col 0 → col 2 (info reads the full Info struct
// for the selected sub).
func (v *SubscriptionsView) SyncDetail() {
	if info, ok := v.listCol.selectedInfo(); ok {
		v.infoCol.set(&info)
	} else {
		v.infoCol.set(nil)
	}
}

// SelectedSubscription — currently-highlighted sub (typed). Used
// by the parent for the `e' editor handoff.
func (v *SubscriptionsView) SelectedSubscription() (subscriptions.Info, bool) {
	return v.listCol.selectedInfo()
}

// Refresh re-fetches the subscription list. Bound to `r' in the
// parent model so users can reload without switching modes.
func (v *SubscriptionsView) Refresh() tea.Cmd {
	v.listCol.loading = true
	return v.listCol.fetch()
}

//------------------------------------------------------------------------------
// Col 0 — subscription list

type subListCol struct {
	api      *subscriptions.Client
	items    []subscriptions.Info
	selected int
	err      error
	loading  bool
	filter   string
	visible  []int
}

func newSubListCol(api *subscriptions.Client) *subListCol {
	return &subListCol{api: api, loading: true}
}

func (s *subListCol) Title() string                       { return "subscriptions" }
func (s *subListCol) Init() tea.Cmd                       { return s.fetch() }
func (s *subListCol) SetParentSelection(string) tea.Cmd   { return nil }
func (s *subListCol) Stop()                               {}

func (s *subListCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(subListLoadedMsg); ok {
		s.items, s.err, s.loading = m.items, m.err, false
		s.selected = clamp(s.selected, 0, max(0, len(s.items)-1))
		return nil, true
	}
	return nil, false
}

func (s *subListCol) Selected() string {
	if info, ok := s.selectedInfo(); ok {
		return info.Name
	}
	return ""
}

func (s *subListCol) selectedInfo() (subscriptions.Info, bool) {
	idx := s.visibleSelected()
	if idx < 0 {
		return subscriptions.Info{}, false
	}
	return s.items[idx], true
}

func (s *subListCol) visibleSelected() int {
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

func (s *subListCol) Move(delta int) {
	n := len(s.items)
	if s.filter != "" {
		n = len(s.visible)
	}
	if n == 0 {
		return
	}
	s.selected = clamp(s.selected+delta, 0, n-1)
}

func (s *subListCol) SetFilter(needle string) {
	s.filter = needle
	if needle == "" {
		s.visible = nil
		return
	}
	names := make([]string, len(s.items))
	for i, it := range s.items {
		names[i] = it.Name + " " + string(it.Type)
	}
	s.visible = filterIndices(names, needle)
	n := len(s.visible)
	if n == 0 {
		s.selected = 0
		return
	}
	s.selected = clamp(s.selected, 0, n-1)
}

func (s *subListCol) GotoID(needle string) bool {
	names := make([]string, len(s.items))
	for i, it := range s.items {
		names[i] = it.Name
	}
	idx := findIndex(names, needle)
	if idx < 0 {
		return false
	}
	s.filter = ""
	s.visible = nil
	s.selected = idx
	return true
}

func (s *subListCol) View(w, h int, active bool) string {
	switch {
	case s.loading:
		return emptyHint("loading…")
	case s.err != nil:
		return errLine(s.err)
	case len(s.items) == 0:
		return emptyHint("no subscriptions")
	}

	nameW := 16
	for _, it := range s.items {
		if l := len(it.Name); l > nameW {
			nameW = l
		}
	}
	if cap := w - 18; nameW > cap && cap > 0 {
		nameW = cap
	}

	rows := make([]string, len(s.items))
	for i, it := range s.items {
		rows[i] = fmt.Sprintf("%s %s %s",
			padRight(it.Name, nameW),
			theme.RowDim.Inline(true).Render(padRight(string(it.Type), 11)),
			theme.RowDim.Inline(true).Render(fmt.Sprintf("ckpt %d", it.Checkpoint)),
		)
	}
	if s.filter != "" {
		filtered := make([]string, 0, len(s.visible))
		for _, i := range s.visible {
			filtered = append(filtered, rows[i])
		}
		if len(filtered) == 0 {
			return emptyHint("no match")
		}
		return renderList(filtered, s.selected, w, h, active)
	}
	return renderList(rows, s.selected, w, h, active)
}

func (s *subListCol) fetch() tea.Cmd {
	api := s.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		items, err := api.List(ctx)
		if err == nil {
			sort.SliceStable(items, func(i, j int) bool {
				return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
			})
		}
		return subListLoadedMsg{items: items, err: err}
	}
}

type subListLoadedMsg struct {
	items []subscriptions.Info
	err   error
}

//------------------------------------------------------------------------------
// Col 1 — lag detail

type subLagCol struct {
	api     *subscriptions.Client
	parent  string
	lag     subscriptions.Lag
	loading bool
	err     error
	loaded  bool
}

func newSubLagCol(api *subscriptions.Client) *subLagCol { return &subLagCol{api: api} }

func (l *subLagCol) Title() string {
	if l.parent == "" {
		return "lag"
	}
	return "lag · " + truncate(l.parent, 22)
}

func (l *subLagCol) Init() tea.Cmd { return nil }

func (l *subLagCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(subLagLoadedMsg); ok {
		if m.name != l.parent {
			return nil, true
		}
		l.lag, l.err, l.loading, l.loaded = m.lag, m.err, false, true
		return nil, true
	}
	return nil, false
}

func (l *subLagCol) SetParentSelection(parent string) tea.Cmd {
	if parent == l.parent {
		return nil
	}
	l.parent = parent
	l.err, l.loaded = nil, false
	if parent == "" {
		l.loading = false
		return nil
	}
	l.loading = true
	api := l.api
	name := parent
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		lag, err := api.Lag(ctx, name)
		return subLagLoadedMsg{name: name, lag: lag, err: err}
	}
}

func (l *subLagCol) Selected() string {
	if !l.loaded {
		return ""
	}
	return l.parent
}

func (l *subLagCol) Move(int)              {}
func (l *subLagCol) SetFilter(string)       {}
func (l *subLagCol) GotoID(string) bool     { return false }
func (l *subLagCol) Stop()                  {}

func (l *subLagCol) View(w, h int, active bool) string {
	switch {
	case l.parent == "":
		return emptyHint("select a subscription →")
	case l.loading:
		return emptyHint("loading…")
	case l.err != nil:
		return errLine(l.err)
	case !l.loaded:
		return emptyHint("—")
	}
	var b strings.Builder
	b.WriteString(kvLine("lag", lagBadge(l.lag.Lag)) + "\n")
	b.WriteString(kvLine("ckpt", fmt.Sprintf("%d", l.lag.CurrentCheckpoint)) + "\n")
	b.WriteString(kvLine("latest", fmt.Sprintf("%d", l.lag.LatestVersion)) + "\n")
	return clip(b.String(), h)
}

func lagBadge(lag uint64) string {
	switch {
	case lag == 0:
		return theme.BadgeOK.Render("0 (caught up)")
	case lag < 100:
		return theme.RowValue.Render(fmt.Sprintf("%d", lag))
	default:
		return theme.BadgeWarn.Render(fmt.Sprintf("%d behind", lag))
	}
}

type subLagLoadedMsg struct {
	name string
	lag  subscriptions.Lag
	err  error
}

//------------------------------------------------------------------------------
// Col 2 — full info (side-channel: parent model calls .set(info))

type subInfoCol struct {
	info *subscriptions.Info
}

func newSubInfoCol() *subInfoCol                              { return &subInfoCol{} }
func (i *subInfoCol) Title() string                           { return "info" }
func (i *subInfoCol) Init() tea.Cmd                           { return nil }
func (i *subInfoCol) Update(tea.Msg) (tea.Cmd, bool)          { return nil, false }
func (i *subInfoCol) SetParentSelection(string) tea.Cmd       { return nil }
func (i *subInfoCol) Selected() string {
	if i.info == nil {
		return ""
	}
	return i.info.ID
}
func (i *subInfoCol) Move(int)                       {}
func (i *subInfoCol) SetFilter(string)                {}
func (i *subInfoCol) GotoID(string) bool              { return false }
func (i *subInfoCol) Stop()                          {}
func (i *subInfoCol) set(info *subscriptions.Info)   { i.info = info }

func (i *subInfoCol) View(w, h int, active bool) string {
	if i.info == nil {
		return emptyHint("—")
	}
	in := i.info
	var b strings.Builder
	b.WriteString(kvLine("id", in.ID) + "\n")
	b.WriteString(kvLine("name", in.Name) + "\n")
	b.WriteString(kvLine("type", string(in.Type)) + "\n")
	if in.Selector != "" {
		b.WriteString(kvLine("selector", in.Selector) + "\n")
	}
	b.WriteString(kvLine("pool", fmt.Sprintf("%d", in.PoolSize)) + "\n")
	b.WriteString(kvLine("ckpt", fmt.Sprintf("%d", in.Checkpoint)) + "\n")
	if !in.CreatedAt.IsZero() {
		b.WriteString(kvLine("created", in.CreatedAt.Format("2006-01-02 15:04:05")) + "\n")
	}
	return clip(b.String(), h)
}

//------------------------------------------------------------------------------
// Snapshots — empty stub gets replaced by the snapshots.go file in
// this package. (Originally subs and snaps were both stubs in the
// same file; the snaps stub now lives in snapshots.go.)
