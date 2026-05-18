package modes

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/genproto/gatewayv1"
	"codeberg.org/reckon-db-org/reckon-go/stores"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/cluster"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ranger"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// StoresView is a 4-pane grid built from two stacked 2-pane rangers:
//
//   ┌──────────────┬──────────────┐
//   │ nodes        │ node detail  │   ← top ranger
//   ├──────────────┼──────────────┤
//   │ stores       │ store info   │   ← bottom ranger
//   └──────────────┴──────────────┘
//
// The bottom-left stores column drives everything: its selection
// IS the model's active store. The top ranger reads that store via
// a getter and shows the nodes hosting it.
//
// tab switches between the two rangers; h/l switches L/R within the
// focused ranger; j/k moves within the focused column.
type StoresView struct {
	client *reckon.Client
	topo   *cluster.Topology

	storesRanger    *ranger.Ranger
	nodesCol     *nodesCol
	nodeDetail   *nodeDetailCol

	nodesRanger *ranger.Ranger
	storesCol    *storesCol
	storeInfo    *storeInfoCol

	focused int // 0 = top, 1 = bottom

	probing string // store id currently being probed
}

// BuildStores wires the 4-pane cluster mode against the shared
// topology and an initial active store. The setStore callback is
// invoked whenever the user's bottom-row cursor commits to a new
// store; main updates m.activeStore so streams/subs/snaps stay in
// sync.
func BuildStores(c *reckon.Client, topo *cluster.Topology, initialStore string, setStore func(string)) *StoresView {
	v := &StoresView{client: c, topo: topo}

	// Forward declaration: the columns need a getter that returns
	// the active store, which is whatever the storesCol cursor is
	// pointing at. We can't construct that before storesCol exists,
	// so we close over v and read storesCol once it's set.
	getStore := func() string {
		if v.storesCol == nil {
			return initialStore
		}
		if s := v.storesCol.Selected(); s != "" {
			return s
		}
		return initialStore
	}

	// Top ranger: stores list + per-store cluster banner. This is
	// the primary scope selector — your first decision when you
	// enter the mode is "which store am I looking at".
	v.storesCol = newStoresCol(topo, initialStore, setStore)
	v.storeInfo = newStoreInfoCol(topo, getStore)
	v.storesRanger = ranger.New2(v.storesCol, v.storeInfo)

	// Bottom ranger: nodes hosting the selected store + per-node
	// detail. Drilled from the top.
	v.nodesCol = newNodesCol(topo, getStore)
	v.nodeDetail = newNodeDetailCol(topo, getStore)
	v.nodesRanger = ranger.New2(v.nodesCol, v.nodeDetail)

	// Start with the stores ranger (top) focused so the user's first
	// j/k touches the store selector — that's the primary decision.
	v.focused = 0
	return v
}

func (v *StoresView) Init() tea.Cmd {
	return tea.Batch(v.storesRanger.Init(), v.nodesRanger.Init())
}

// HandleKey processes a navigation key. tab switches rangers;
// other keys delegate to the focused ranger. If the top-left
// store cursor commits to a different store, a fresh probe fires
// immediately (otherwise the cluster banner would stay stale until
// the 5s tick).
func (v *StoresView) HandleKey(key string) (tea.Cmd, bool) {
	switch key {
	case "tab":
		v.focused = (v.focused + 1) % 2
		return nil, true
	case "shift+tab":
		v.focused = (v.focused + 1) % 2 // only 2 rangers
		return nil, true
	}
	prev := v.storesCol.Selected()
	cmd, handled := v.FocusedRanger().HandleKey(key)
	if now := v.storesCol.Selected(); now != prev && now != "" {
		cmd = tea.Batch(cmd, v.HealthProbeCmd())
	}
	return cmd, handled
}

// Update fans the message through both rangers + handles
// healthProbeMsg/healthTickMsg at this level.
func (v *StoresView) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case healthProbeMsg:
		v.topo.PutHealth(m.storeID, m.health)
		if v.probing == m.storeID {
			v.probing = ""
		}
		return nil
	case healthTickMsg:
		return tea.Batch(v.HealthProbeCmd(), healthTick())
	}
	return tea.Batch(v.storesRanger.Update(msg), v.nodesRanger.Update(msg))
}

// SyncDetail copies col selections into the detail columns.
func (v *StoresView) SyncDetail() {
	if ev, ok := v.nodesCol.selectedNode(); ok {
		v.nodeDetail.setNode(&ev)
	} else {
		v.nodeDetail.setNode(nil)
	}
}

// View renders the 4-pane grid at (width, height). Splits height
// 50/50 between top and bottom; each ranger handles its own L/R
// split via the ranger code.
func (v *StoresView) View(width, height int) string {
	topH := height / 2
	botH := height - topH
	top := v.storesRanger.View(width, topH)
	bot := v.nodesRanger.View(width, botH)
	return lipgloss.JoinVertical(lipgloss.Left, top, bot)
}

func (v *StoresView) Stop() {
	v.storesRanger.Stop()
	v.nodesRanger.Stop()
}

// HealthProbeCmd fires a probe for the active store. Called on
// mode entry, on the 5s tick, and after topology changes.
func (v *StoresView) HealthProbeCmd() tea.Cmd {
	storeID := ""
	if v.storesCol != nil {
		storeID = v.storesCol.Selected()
	}
	if storeID == "" || v.probing == storeID {
		return nil
	}
	v.probing = storeID
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		return healthProbeMsg{storeID: storeID, health: probeHealth(ctx, client, storeID)}
	}
}

// SelectedStore — id of the top-left's current selection. Used by
// main.go to keep m.activeStore in lockstep after the stores mode
// runs its key handling, and as the target for `enter' → jump to
// streams mode.
func (v *StoresView) SelectedStore() string {
	if v.storesCol == nil {
		return ""
	}
	return v.storesCol.Selected()
}

// FocusedIndex — 0 = stores (top), 1 = nodes (bottom). Exposed
// for the status bar.
func (v *StoresView) FocusedIndex() int { return v.focused }

// IsStoresFocused — true when the stores ranger (top) currently
// has focus. Use this from main.go's key handlers instead of
// hard-coding the focus index.
func (v *StoresView) IsStoresFocused() bool { return v.focused == 0 }

// FocusedRanger returns the inner ranger that currently has focus.
// Used by the global filter/goto plumbing in main.go.
func (v *StoresView) FocusedRanger() *ranger.Ranger {
	if v.focused == 1 {
		return v.nodesRanger
	}
	return v.storesRanger
}

//------------------------------------------------------------------------------
// Top-left — nodes hosting the active store

type nodesCol struct {
	topo     *cluster.Topology
	getStore func() string
	selected int
	lastSeen string
}

func newNodesCol(topo *cluster.Topology, getStore func() string) *nodesCol {
	return &nodesCol{topo: topo, getStore: getStore}
}

func (n *nodesCol) Title() string {
	store := n.getStore()
	if store == "" {
		return "nodes"
	}
	return "nodes · " + truncate(store, 22)
}

func (n *nodesCol) Init() tea.Cmd                     { return nil }
func (n *nodesCol) Update(tea.Msg) (tea.Cmd, bool)    { return nil, false }
func (n *nodesCol) SetParentSelection(string) tea.Cmd { return nil }

func (n *nodesCol) Selected() string {
	if ev, ok := n.selectedNode(); ok {
		return ev.Node
	}
	return ""
}

func (n *nodesCol) selectedNode() (stores.Instance, bool) {
	store := n.getStore()
	if n.lastSeen != store {
		n.selected = 0
		n.lastSeen = store
	}
	nodes := n.topo.NodesFor(store)
	if n.selected < 0 || n.selected >= len(nodes) {
		return stores.Instance{}, false
	}
	return nodes[n.selected], true
}

func (n *nodesCol) Move(delta int) {
	nodes := n.topo.NodesFor(n.getStore())
	if len(nodes) == 0 {
		return
	}
	n.selected = clamp(n.selected+delta, 0, len(nodes)-1)
}

// Stores mode lists are derived from live topology + tiny; filter is
// noise. Goto by node name works.
func (n *nodesCol) SetFilter(string) {}

func (n *nodesCol) GotoID(needle string) bool {
	if needle == "" {
		return false
	}
	nodes := n.topo.NodesFor(n.getStore())
	needle = strings.ToLower(needle)
	for i, inst := range nodes {
		if strings.Contains(strings.ToLower(inst.Node), needle) {
			n.selected = i
			return true
		}
	}
	return false
}

func (n *nodesCol) View(w, h int, active bool) string {
	store := n.getStore()
	if store == "" {
		return emptyHint("no active store")
	}
	if n.lastSeen != store {
		n.selected = 0
		n.lastSeen = store
	}
	nodes := n.topo.NodesFor(store)
	if len(nodes) == 0 {
		return emptyHint("no nodes")
	}
	hp := n.topo.Health(store)

	rows := make([]string, len(nodes))
	for i, inst := range nodes {
		leader := ""
		if inst.Node == hp.Leader {
			leader = "  " + theme.BadgeOK.Inline(true).Render("leader")
		}
		rows[i] = fmt.Sprintf("%s %s%s",
			padRight(shortNodeName(inst.Node), 18),
			theme.RowDim.Inline(true).Render(padRight(string(inst.Mode), 8)),
			leader,
		)
	}
	return renderList(rows, n.selected, w, h, active)
}

func (n *nodesCol) Stop() {}

//------------------------------------------------------------------------------
// Top-right — node detail (per-instance)

type nodeDetailCol struct {
	topo     *cluster.Topology
	getStore func() string
	node     *stores.Instance
}

func newNodeDetailCol(topo *cluster.Topology, getStore func() string) *nodeDetailCol {
	return &nodeDetailCol{topo: topo, getStore: getStore}
}

func (d *nodeDetailCol) Title() string                     { return "node detail" }
func (d *nodeDetailCol) Init() tea.Cmd                     { return nil }
func (d *nodeDetailCol) Update(tea.Msg) (tea.Cmd, bool)    { return nil, false }
func (d *nodeDetailCol) SetParentSelection(string) tea.Cmd { return nil }
func (d *nodeDetailCol) Selected() string {
	if d.node == nil {
		return ""
	}
	return d.node.Node
}
func (d *nodeDetailCol) Move(int)                       {}
func (d *nodeDetailCol) SetFilter(string)                {}
func (d *nodeDetailCol) GotoID(string) bool              { return false }
func (d *nodeDetailCol) Stop()                          {}
func (d *nodeDetailCol) setNode(n *stores.Instance)     { d.node = n }

func (d *nodeDetailCol) View(w, h int, active bool) string {
	if d.node == nil {
		return emptyHint("select a node →")
	}
	store := d.getStore()
	hp := d.topo.Health(store)
	var b strings.Builder

	b.WriteString(kvLine("name", d.node.Node) + "\n")
	role := "follower"
	if hp.Leader == d.node.Node {
		role = "leader ★"
	}
	b.WriteString(kvLine("role", role) + "\n")
	b.WriteString(kvLine("mode", string(d.node.Mode)) + "\n")
	b.WriteString(kvLine("data_dir", d.node.DataDir) + "\n")
	if !d.node.RegisteredAt.IsZero() {
		b.WriteString(kvLine("up since", humanAgo(d.node.RegisteredAt)) + "\n")
	}
	if d.node.Timeout > 0 {
		b.WriteString(kvLine("rpc_t/o", d.node.Timeout.String()) + "\n")
	}
	return clip(b.String(), h)
}

//------------------------------------------------------------------------------
// Bottom-left — stores list

type storesCol struct {
	topo     *cluster.Topology
	setStore func(string)

	// We track our own selection as a store id rather than an index
	// because Topology.Stores() can grow/reorder when WatchStores
	// fires; sticking to a stable id keeps the cursor on the same
	// store across re-renders.
	selectedID string
}

func newStoresCol(topo *cluster.Topology, initial string, setStore func(string)) *storesCol {
	return &storesCol{topo: topo, setStore: setStore, selectedID: initial}
}

func (s *storesCol) Title() string                     { return "stores" }
func (s *storesCol) Init() tea.Cmd                     { return nil }
func (s *storesCol) Update(tea.Msg) (tea.Cmd, bool)    { return nil, false }
func (s *storesCol) SetParentSelection(string) tea.Cmd { return nil }
func (s *storesCol) Stop()                             {}

func (s *storesCol) Selected() string {
	// If the remembered id is gone (store retired), fall back to
	// the first available so View doesn't render an orphan cursor.
	store := s.resolveSelected()
	return store
}

func (s *storesCol) resolveSelected() string {
	all := s.topo.Stores()
	if len(all) == 0 {
		return ""
	}
	for _, id := range all {
		if id == s.selectedID {
			return id
		}
	}
	s.selectedID = all[0]
	if s.setStore != nil {
		s.setStore(s.selectedID)
	}
	return s.selectedID
}

func (s *storesCol) Move(delta int) {
	all := s.topo.Stores()
	if len(all) == 0 {
		return
	}
	idx := 0
	for i, id := range all {
		if id == s.selectedID {
			idx = i
			break
		}
	}
	idx = clamp(idx+delta, 0, len(all)-1)
	if all[idx] != s.selectedID {
		s.selectedID = all[idx]
		if s.setStore != nil {
			s.setStore(s.selectedID)
		}
	}
}

// Tiny topology-derived list; filter would just hide stores from the
// dashboard. Goto by store id substring works.
func (s *storesCol) SetFilter(string) {}

func (s *storesCol) GotoID(needle string) bool {
	if needle == "" {
		return false
	}
	all := s.topo.Stores()
	needle = strings.ToLower(needle)
	for _, id := range all {
		if strings.Contains(strings.ToLower(id), needle) {
			if id != s.selectedID {
				s.selectedID = id
				if s.setStore != nil {
					s.setStore(id)
				}
			}
			return true
		}
	}
	return false
}

func (s *storesCol) View(w, h int, active bool) string {
	all := s.topo.Stores()
	if len(all) == 0 {
		if err := s.topo.Err(); err != nil {
			return errLine(err)
		}
		return emptyHint("discovering stores…")
	}

	// Pad ids to the longest, capped to fit a trailing chip.
	idW := longestLen(all)
	if cap := w - 14; idW > cap && cap > 0 {
		idW = cap
	}
	if idW < 8 {
		idW = 8
	}

	selIdx := 0
	for i, id := range all {
		if id == s.selectedID {
			selIdx = i
			break
		}
	}

	rows := make([]string, len(all))
	for i, id := range all {
		nodes := s.topo.NodeCountFor(id)
		hp := s.topo.Health(id)
		dot := theme.RowDim.Inline(true).Render("·")
		if !hp.LastProbed.IsZero() {
			if hp.OK {
				dot = theme.BadgeOK.Inline(true).Render("●")
			} else {
				dot = theme.BadgeError.Inline(true).Render("●")
			}
		}
		rows[i] = fmt.Sprintf("%s %s %s",
			dot,
			padRight(id, idW),
			theme.RowDim.Inline(true).Render(fmt.Sprintf("%d node(s)", nodes)),
		)
	}
	return renderList(rows, selIdx, w, h, active)
}

//------------------------------------------------------------------------------
// Bottom-right — store info (cluster banner)

type storeInfoCol struct {
	topo     *cluster.Topology
	getStore func() string
}

func newStoreInfoCol(topo *cluster.Topology, getStore func() string) *storeInfoCol {
	return &storeInfoCol{topo: topo, getStore: getStore}
}

func (i *storeInfoCol) Title() string                     { return "store info" }
func (i *storeInfoCol) Init() tea.Cmd                     { return nil }
func (i *storeInfoCol) Update(tea.Msg) (tea.Cmd, bool)    { return nil, false }
func (i *storeInfoCol) SetParentSelection(string) tea.Cmd { return nil }
func (i *storeInfoCol) Selected() string                  { return i.getStore() }
func (i *storeInfoCol) Move(int)                          {}
func (i *storeInfoCol) SetFilter(string)                   {}
func (i *storeInfoCol) GotoID(string) bool                 { return false }
func (i *storeInfoCol) Stop()                             {}

func (i *storeInfoCol) View(w, h int, active bool) string {
	store := i.getStore()
	if store == "" {
		return emptyHint("pick a store →")
	}
	hp := i.topo.Health(store)
	var b strings.Builder

	if hp.LastProbed.IsZero() {
		b.WriteString(emptyHint("probing health…"))
		return clip(b.String(), h)
	}

	b.WriteString(kvLine("status", clusterStatus(hp)) + "\n")
	if hp.Leader != "" {
		b.WriteString(kvLine("leader", shortNodeName(hp.Leader)) + "\n")
	}
	if hp.NodesTotal > 0 {
		b.WriteString(kvLine("quorum", fmt.Sprintf("%d/%d up", hp.NodesUp, hp.NodesTotal)) + "\n")
	}
	if hp.Term > 0 {
		b.WriteString(kvLine("term", fmt.Sprintf("%d", hp.Term)) + "\n")
	}
	b.WriteString(kvLine("lag", fmt.Sprintf("%d", hp.MaxCommitLag)) + "\n")
	if len(hp.FailedNodes) > 0 {
		b.WriteString(kvLine("failed", strings.Join(hp.FailedNodes, ", ")) + "\n")
	}
	if hp.LastProbeErr != nil {
		b.WriteString(theme.BadgeError.Render("probe: ") +
			theme.RowValue.Render(hp.LastProbeErr.Error()) + "\n")
	}
	return clip(b.String(), h)
}

//------------------------------------------------------------------------------
// HealthService probe

type healthProbeMsg struct {
	storeID string
	health  cluster.StoreHealth
}

type healthTickMsg struct{}

func healthTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return healthTickMsg{} })
}

// HealthTick — public entry used by the top-level model's Init.
func HealthTick() tea.Cmd { return healthTick() }

func probeHealth(ctx context.Context, c *reckon.Client, storeID string) cluster.StoreHealth {
	h := cluster.StoreHealth{LastProbed: time.Now()}
	hc := gatewayv1.NewHealthServiceClient(c.Conn())

	if resp, err := hc.VerifyClusterConsistency(ctx,
		&gatewayv1.ClusterCheckRequest{StoreId: storeID}); err == nil {
		details := resp.GetDetails()
		h.Status = details["status"]
		h.OK = h.Status == "healthy"
		if leader := details["leader"]; leader != "" {
			h.Leader = cluster.ParseLeader(leader)
		}
		if quorum := details["quorum"]; quorum != "" {
			h.NodesUp = int(extractField(quorum, "available_nodes"))
			h.NodesTotal = int(extractField(quorum, "total_nodes"))
			h.HasQuorum = strings.Contains(quorum, "has_quorum => true")
		}
		if membership := details["membership"]; membership != "" {
			h.FailedNodes = parseFailedNodesList(membership)
		}
	} else {
		h.LastProbeErr = err
	}

	if resp, err := hc.CheckRaftLogConsistency(ctx,
		&gatewayv1.ClusterCheckRequest{StoreId: storeID}); err == nil {
		details := resp.GetDetails()
		if terms := details["terms"]; terms != "" {
			h.Term = cluster.ParseFirstInt(terms)
		}
		if lag := details["max_commit_lag"]; lag != "" {
			h.MaxCommitLag = cluster.ParseFirstInt(lag)
		}
		if h.Leader == "" {
			h.Leader = cluster.ParseLeader(details["leader"])
		}
	} else if h.LastProbeErr == nil {
		h.LastProbeErr = err
	}

	return h
}

func extractField(s, key string) int64 {
	idx := strings.Index(s, key+" =>")
	if idx < 0 {
		return 0
	}
	rest := s[idx+len(key)+3:]
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '=' || rest[0] == '>') {
		rest = rest[1:]
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	var n int64
	for i := 0; i < end; i++ {
		n = n*10 + int64(rest[i]-'0')
	}
	return n
}

func parseFailedNodesList(membership string) []string {
	idx := strings.Index(membership, "failed_nodes =>")
	if idx < 0 {
		return nil
	}
	rest := membership[idx+len("failed_nodes =>"):]
	open := strings.Index(rest, "[")
	close := strings.Index(rest, "]")
	if open < 0 || close < 0 || close <= open {
		return nil
	}
	return cluster.ParseFailedNodes(rest[open : close+1])
}

func shortNodeName(s string) string {
	if i := strings.Index(s, "@"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func clusterStatus(h cluster.StoreHealth) string {
	switch {
	case h.OK:
		return theme.BadgeOK.Render(h.Status)
	case h.Status != "":
		return theme.BadgeWarn.Render(h.Status)
	default:
		return theme.RowDim.Render("unknown")
	}
}
