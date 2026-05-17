package modes

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/genproto/gatewayv1"
	"codeberg.org/reckon-db-org/reckon-go/stores"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/cluster"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ranger"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// ClusterView is the wired ranger triple for the cluster mode.
//
// Col 1: stores known to the cluster (from WatchStores topology)
// Col 2: nodes hosting the selected store (leader marked ★)
// Col 3: per-node detail (mode, data_dir, registered) + cluster
//        banner (leader, term, quorum, failed nodes)
//
// Health snapshots are populated via async HealthService probes
// fired when the selected store changes. The columns read the
// shared *cluster.Topology so the data is consistent with the
// header.
type ClusterView struct {
	Ranger    *ranger.Ranger
	storesCol *clusterStoresCol
	nodesCol  *clusterNodesCol
	detailCol *clusterDetailCol
}

// BuildCluster wires the cluster mode against the live client and
// a shared topology (populated by the top-level WatchStores poller).
func BuildCluster(c *reckon.Client, topo *cluster.Topology) *ClusterView {
	storesCol := newClusterStoresCol(c, topo)
	nodesCol := newClusterNodesCol(topo)
	detailCol := newClusterDetailCol(topo)
	return &ClusterView{
		Ranger:    ranger.New(storesCol, nodesCol, detailCol),
		storesCol: storesCol,
		nodesCol:  nodesCol,
		detailCol: detailCol,
	}
}

// SyncDetail wires col 2's selection into col 3 (the detail view
// needs the selected node, which isn't reachable via SetParentSelection
// alone — that just carries the row id, not the typed instance).
func (v *ClusterView) SyncDetail() {
	if ev, ok := v.nodesCol.selectedNode(); ok {
		v.detailCol.set(v.storesCol.Selected(), &ev)
	} else {
		v.detailCol.set(v.storesCol.Selected(), nil)
	}
}

//------------------------------------------------------------------------------
// Column 1 — stores list. Also owns the HealthService probe pump.

type clusterStoresCol struct {
	client   *reckon.Client
	topo     *cluster.Topology
	selected int
	probing  string // store_id we're currently probing
}

func newClusterStoresCol(c *reckon.Client, topo *cluster.Topology) *clusterStoresCol {
	return &clusterStoresCol{client: c, topo: topo}
}

func (s *clusterStoresCol) Title() string { return "stores" }

func (s *clusterStoresCol) Init() tea.Cmd {
	// Fire an initial probe once a store appears. The Update fan-out
	// + Move handler take it from there.
	return nil
}

func (s *clusterStoresCol) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case healthProbeMsg:
		s.topo.PutHealth(m.storeID, m.health)
		if s.probing == m.storeID {
			s.probing = ""
		}
		return nil, true
	case healthTickMsg:
		// Periodic refresh of the focused store's health.
		return s.probe(), true
	}
	return nil, false
}

func (s *clusterStoresCol) SetParentSelection(string) tea.Cmd { return nil }

func (s *clusterStoresCol) Selected() string {
	stores := s.topo.Stores()
	if len(stores) == 0 || s.selected < 0 || s.selected >= len(stores) {
		return ""
	}
	return stores[s.selected]
}

func (s *clusterStoresCol) Move(delta int) {
	stores := s.topo.Stores()
	if len(stores) == 0 {
		return
	}
	s.selected = clamp(s.selected+delta, 0, len(stores)-1)
	// Selection changed — kick a probe if we don't already have a
	// fresh-enough snapshot.
}

func (s *clusterStoresCol) View(w, h int, active bool) string {
	stores := s.topo.Stores()
	if len(stores) == 0 {
		if err := s.topo.Err(); err != nil {
			return errLine(err)
		}
		return emptyHint("discovering stores…")
	}

	// Pad ids to the longest id width (so trailing chips align
	// across rows), capped at w-12 so a long id can't squeeze
	// the chip off-screen.
	idW := longestLen(stores)
	if cap := w - 12; idW > cap && cap > 0 {
		idW = cap
	}
	if idW < 8 {
		idW = 8
	}

	rows := make([]string, len(stores))
	for i, id := range stores {
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
	return renderList(rows, s.selected, w, h, active)
}

func (s *clusterStoresCol) Stop() {}

// probe fires a HealthService consistency check on the selected
// store. We use VerifyClusterConsistency for leader+quorum and
// CheckRaftLogConsistency for term+lag, merging the results.
func (s *clusterStoresCol) probe() tea.Cmd {
	storeID := s.Selected()
	if storeID == "" || s.probing == storeID {
		return nil
	}
	s.probing = storeID
	client := s.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortRPCTimeout)
		defer cancel()
		h := probeHealth(ctx, client, storeID)
		return healthProbeMsg{storeID: storeID, health: h}
	}
}

// HealthProbeCmd is the public hook the top-level model uses to
// kick a probe (e.g. on mode switch + on selection change).
func (v *ClusterView) HealthProbeCmd() tea.Cmd { return v.storesCol.probe() }

// SelectedStore — id of the store currently highlighted in col 1,
// or "". Used by the top-level model's enter handler to jump into
// streams mode bound to this store.
func (v *ClusterView) SelectedStore() string { return v.storesCol.Selected() }

// Focus — current ranger focus index (0/1/2). Exposed so the parent
// model can decide whether enter means "jump to streams" (col 0) or
// "descend within ranger" (col 1/2).
func (v *ClusterView) Focus() int { return v.Ranger.Focused() }

//------------------------------------------------------------------------------
// Column 2 — nodes hosting the selected store

type clusterNodesCol struct {
	topo     *cluster.Topology
	parent   string // selected store_id
	selected int
}

func newClusterNodesCol(topo *cluster.Topology) *clusterNodesCol {
	return &clusterNodesCol{topo: topo}
}

func (n *clusterNodesCol) Title() string {
	if n.parent == "" {
		return "nodes"
	}
	return "nodes · " + truncate(n.parent, 22)
}

func (n *clusterNodesCol) Init() tea.Cmd                  { return nil }
func (n *clusterNodesCol) Update(tea.Msg) (tea.Cmd, bool) { return nil, false }

func (n *clusterNodesCol) SetParentSelection(parent string) tea.Cmd {
	if parent != n.parent {
		n.parent = parent
		n.selected = 0
	}
	return nil
}

func (n *clusterNodesCol) Selected() string {
	if ev, ok := n.selectedNode(); ok {
		return ev.Node
	}
	return ""
}

func (n *clusterNodesCol) selectedNode() (stores.Instance, bool) {
	nodes := n.topo.NodesFor(n.parent)
	if n.selected < 0 || n.selected >= len(nodes) {
		return stores.Instance{}, false
	}
	return nodes[n.selected], true
}

func (n *clusterNodesCol) Move(delta int) {
	nodes := n.topo.NodesFor(n.parent)
	if len(nodes) == 0 {
		return
	}
	n.selected = clamp(n.selected+delta, 0, len(nodes)-1)
}

func (n *clusterNodesCol) View(w, h int, active bool) string {
	if n.parent == "" {
		return emptyHint("select a store →")
	}
	nodes := n.topo.NodesFor(n.parent)
	if len(nodes) == 0 {
		return emptyHint("no nodes")
	}
	hp := n.topo.Health(n.parent)
	rows := make([]string, len(nodes))
	for i, inst := range nodes {
		// Leader marker hangs at the end, after the mode chip, so
		// the IP column stays aligned regardless of terminal star-
		// glyph width quirks.
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

func (n *clusterNodesCol) Stop() {}

//------------------------------------------------------------------------------
// Column 3 — node detail + cluster banner

type clusterDetailCol struct {
	topo *cluster.Topology

	storeID string
	node    *stores.Instance
}

func newClusterDetailCol(topo *cluster.Topology) *clusterDetailCol {
	return &clusterDetailCol{topo: topo}
}

func (d *clusterDetailCol) Title() string                          { return "detail" }
func (d *clusterDetailCol) Init() tea.Cmd                          { return nil }
func (d *clusterDetailCol) Update(tea.Msg) (tea.Cmd, bool)         { return nil, false }
func (d *clusterDetailCol) SetParentSelection(string) tea.Cmd      { return nil }
func (d *clusterDetailCol) Selected() string {
	if d.node == nil {
		return ""
	}
	return d.node.Node
}
func (d *clusterDetailCol) Move(int) {}
func (d *clusterDetailCol) Stop()    {}

func (d *clusterDetailCol) set(storeID string, node *stores.Instance) {
	d.storeID = storeID
	d.node = node
}

func (d *clusterDetailCol) View(w, h int, active bool) string {
	if d.storeID == "" {
		return emptyHint("pick a store →")
	}
	hp := d.topo.Health(d.storeID)
	var b strings.Builder

	// Cluster banner — applies to the whole store, drawn regardless
	// of which node is selected.
	b.WriteString(theme.PaneTitle.Render("cluster") + "\n")
	if hp.LastProbed.IsZero() {
		b.WriteString(emptyHint("probing health…") + "\n")
	} else {
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
	}

	if d.node != nil {
		b.WriteString("\n" + theme.PaneTitle.Render("node") + "\n")
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
	}
	return clip(b.String(), h)
}

//------------------------------------------------------------------------------
// HealthService probe — fired by storesCol.

type healthProbeMsg struct {
	storeID string
	health  cluster.StoreHealth
}

type healthTickMsg struct{}

func probeHealth(ctx context.Context, c *reckon.Client, storeID string) cluster.StoreHealth {
	h := cluster.StoreHealth{LastProbed: time.Now()}
	hc := gatewayv1.NewHealthServiceClient(c.Conn())

	// VerifyClusterConsistency — leader + per-aspect statuses
	if resp, err := hc.VerifyClusterConsistency(ctx,
		&gatewayv1.ClusterCheckRequest{StoreId: storeID}); err == nil {
		details := resp.GetDetails()
		h.Status = details["status"]
		h.OK = h.Status == "healthy"
		if leader := details["leader"]; leader != "" {
			h.Leader = cluster.ParseLeader(leader)
		}
		if quorum := details["quorum"]; quorum != "" {
			// quorum value is "#{required_quorum => 3,available_nodes => 4,..."
			// We pluck "available_nodes => X" and "total_nodes => Y".
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

	// CheckRaftLogConsistency — term + lag
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

// extractField parses "key => N" inside an Erlang-formatted map
// string. Returns 0 if not found.
func extractField(s, key string) int64 {
	idx := strings.Index(s, key+" =>")
	if idx < 0 {
		return 0
	}
	rest := s[idx+len(key)+3:]
	// trim leading whitespace + arrow
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '=' || rest[0] == '>') {
		rest = rest[1:]
	}
	// take leading digits
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

// parseFailedNodesList extracts the failed_nodes => [...] segment
// then returns the 'node@host' atoms inside it. Empty membership
// strings or empty failed_nodes return nil.
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

// HealthTick — emit periodically from the top-level so the cluster
// banner refreshes itself.
func HealthTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return healthTickMsg{}
	})
}

// shortNodeName — reckon_gateway@192.168.1.12 → 192.168.1.12, ""
// passthrough. We keep the host part (post-@) since that's the part
// users care about; the BEAM nodename prefix is always
// reckon_gateway in this deployment.
func shortNodeName(s string) string {
	if i := strings.Index(s, "@"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// clusterStatus — render the textual status with a colour.
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
