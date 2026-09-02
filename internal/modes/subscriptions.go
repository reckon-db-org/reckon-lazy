package modes

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	reckon "github.com/reckon-db-org/reckon-go"
	"github.com/reckon-db-org/reckon-go/subscriptions"
	"github.com/reckon-db-org/reckon-lazy/internal/ranger"
	"github.com/reckon-db-org/reckon-lazy/internal/theme"
)

// SubscriptionsView — 2-level drill:
//
//	Level 0: subscription list (name + type + checkpoint chip)
//	Level 1: detail — full info (id, type, selector, pool, ckpt,
//	         created_at) plus live lag (GetSubscriptionLag)
//
// Lag and info are co-details of one selected subscription, not a
// hierarchy, so they share a single full-width detail pane rather
// than two cramped columns. Data comes from SubscriptionService.List
// + GetSubscriptionLag; lag refetches whenever the selection changes.
type SubscriptionsView struct {
	Drill     *ranger.Drill
	listCol   *subListCol
	detailCol *subDetailCol
}

func BuildSubscriptions(c *reckon.Client, store string) *SubscriptionsView {
	api := c.Subscriptions(store)
	listCol := newSubListCol(api)
	detailCol := newSubDetailCol(api, func() *subscriptions.Info {
		if info, ok := listCol.selectedInfo(); ok {
			return &info
		}
		return nil
	})
	return &SubscriptionsView{
		Drill:     ranger.NewDrill(listCol, detailCol),
		listCol:   listCol,
		detailCol: detailCol,
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

func (s *subListCol) Title() string                     { return "subscriptions" }
func (s *subListCol) Init() tea.Cmd                     { return s.fetch() }
func (s *subListCol) SetParentSelection(string) tea.Cmd { return nil }
func (s *subListCol) Stop()                             {}
func (s *subListCol) Crumb() string                     { return s.Selected() } // selected sub name

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
// Level 1 — combined detail: full info (read live from the list
// selection via a closure) + lag (fetched from GetSubscriptionLag on
// every selection change).

type subDetailCol struct {
	api    *subscriptions.Client
	source func() *subscriptions.Info

	parent  string // sub name the lag below is for
	lag     subscriptions.Lag
	loading bool
	loaded  bool
	err     error
}

func newSubDetailCol(api *subscriptions.Client, src func() *subscriptions.Info) *subDetailCol {
	return &subDetailCol{api: api, source: src}
}

func (d *subDetailCol) Title() string      { return "detail" }
func (d *subDetailCol) Init() tea.Cmd      { return nil }
func (d *subDetailCol) Move(int)           {}
func (d *subDetailCol) SetFilter(string)   {}
func (d *subDetailCol) GotoID(string) bool { return false }
func (d *subDetailCol) Stop()              {}
func (d *subDetailCol) Crumb() string      { return "" } // leaf: no breadcrumb segment

func (d *subDetailCol) Selected() string {
	if info := d.source(); info != nil {
		return info.ID
	}
	return ""
}

func (d *subDetailCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(subLagLoadedMsg); ok {
		if m.name != d.parent {
			return nil, true // stale (selection changed while RPC in flight)
		}
		d.lag, d.err, d.loading, d.loaded = m.lag, m.err, false, true
		return nil, true
	}
	return nil, false
}

// SetParentSelection fires a lag fetch whenever the selected sub
// changes. The info half needs no fetch — it is read live from the
// closure at View time.
func (d *subDetailCol) SetParentSelection(parent string) tea.Cmd {
	if parent == d.parent {
		return nil
	}
	d.parent = parent
	d.err, d.loaded = nil, false
	if parent == "" {
		d.loading = false
		return nil
	}
	d.loading = true
	api := d.api
	name := parent
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		lag, err := api.Lag(ctx, name)
		return subLagLoadedMsg{name: name, lag: lag, err: err}
	}
}

func (d *subDetailCol) View(w, h int, active bool) string {
	info := d.source()
	if info == nil {
		return emptyHint("select a subscription →")
	}
	var b strings.Builder
	b.WriteString(kvLine("id", info.ID) + "\n")
	b.WriteString(kvLine("name", info.Name) + "\n")
	b.WriteString(kvLine("type", string(info.Type)) + "\n")
	if info.Selector != "" {
		b.WriteString(kvLine("selector", info.Selector) + "\n")
	}
	b.WriteString(kvLine("pool", fmt.Sprintf("%d", info.PoolSize)) + "\n")
	if !info.CreatedAt.IsZero() {
		b.WriteString(kvLine("created", info.CreatedAt.Format("2006-01-02 15:04:05")) + "\n")
	}

	b.WriteString("\n" + theme.RowHeader.Render("lag") + "\n")
	switch {
	case d.loading:
		b.WriteString(emptyHint("loading…"))
	case d.err != nil:
		b.WriteString(errLine(d.err))
	case !d.loaded:
		b.WriteString(emptyHint("—"))
	default:
		b.WriteString(kvLine("behind", lagBadge(d.lag.Lag)) + "\n")
		b.WriteString(kvLine("ckpt", fmt.Sprintf("%d", d.lag.CurrentCheckpoint)) + "\n")
		b.WriteString(kvLine("latest", fmt.Sprintf("%d", d.lag.LatestVersion)))
	}
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
