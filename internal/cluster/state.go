// Package cluster holds the shared cluster-state view used by the
// top-level header and the cluster mode. One source of truth, two
// readers: the header asks "what's the rough health", the cluster
// mode asks "give me the per-store node list with leader markers."
package cluster

import (
	"regexp"
	"sort"
	"sync"
	"time"

	"codeberg.org/reckon-db-org/reckon-go/stores"
)

// Topology tracks every (store_id, node) registration observed
// from StoresService.WatchStores plus the most-recent
// HealthService-derived facts per store. Mutations come from the
// top-level model on the tea.Msg path; reads come from views.
// A mutex keeps it safe if a future feature reads from a goroutine.
type Topology struct {
	mu sync.RWMutex

	// instances keyed by store_id+"@"+node
	instances map[string]stores.Instance

	// per-store health snapshot (leader, term, quorum)
	health map[string]StoreHealth

	// lifetime counters (debug + header)
	events int
	err    error
}

// StoreHealth — what we've learned about one store's cluster state.
// Populated by the cluster mode when it fires HealthService probes
// for the focused store; read by both cluster mode columns + the
// header.
type StoreHealth struct {
	Leader        string // e.g. "reckon_gateway@192.168.1.12", "" if unknown
	Term          int64  // Raft term; 0 if unknown
	HasQuorum     bool
	NodesUp       int
	NodesTotal    int
	MaxCommitLag  int64
	FailedNodes   []string
	OK            bool   // overall status (healthy vs split-brain/etc)
	Status        string // "healthy", "split_brain", ...
	LastProbed    time.Time
	LastProbeErr  error
}

// New returns an empty Topology ready for use.
func New() *Topology {
	return &Topology{
		instances: map[string]stores.Instance{},
		health:    map[string]StoreHealth{},
	}
}

// ApplyEvent records one StoresService.WatchStores event.
func (t *Topology) ApplyEvent(ev stores.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := ev.Instance.StoreID + "@" + ev.Instance.Node
	if ev.Type == stores.EventRetired {
		delete(t.instances, key)
	} else {
		t.instances[key] = ev.Instance
	}
	t.events++
}

// SetError records a stream-level error from WatchStores.
func (t *Topology) SetError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err
}

// PutHealth stores the latest per-store health snapshot.
func (t *Topology) PutHealth(storeID string, h StoreHealth) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.health[storeID] = h
}

// Stores returns the list of distinct store IDs, sorted.
func (t *Topology) Stores() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	seen := map[string]struct{}{}
	for _, i := range t.instances {
		seen[i.StoreID] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// NodesFor returns the instances of storeID sorted by node name.
func (t *Topology) NodesFor(storeID string) []stores.Instance {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []stores.Instance
	for _, i := range t.instances {
		if i.StoreID == storeID {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Node < out[j].Node
	})
	return out
}

// Health returns the latest snapshot for storeID, or zero if none.
func (t *Topology) Health(storeID string) StoreHealth {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.health[storeID]
}

// NodeCount returns the total number of distinct (store, node)
// instances across all stores. Useful for header summaries.
func (t *Topology) NodeCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.instances)
}

// NodeCountFor — how many nodes host storeID.
func (t *Topology) NodeCountFor(storeID string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	n := 0
	for _, i := range t.instances {
		if i.StoreID == storeID {
			n++
		}
	}
	return n
}

// Events — total events processed (for debug).
func (t *Topology) Events() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.events
}

// Err — most-recent stream error, if any.
func (t *Topology) Err() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.err
}

//------------------------------------------------------------------------------
// Health-RPC result parsing
//
// The HealthService RPCs return Erlang-formatted strings inside a
// details map (e.g.  leader => 'reckon_gateway@192.168.1.12'). These
// helpers pull the fields we want out without us reimplementing an
// Erlang term parser.

var (
	leaderRE      = regexp.MustCompile(`leader\s*=>\s*'([^']+)'`)
	leaderTupleRE = regexp.MustCompile(`\{[^,]+,'([^']+)'\}`)
	termsRE       = regexp.MustCompile(`\[([^\]]*)\]`)
	maxLagRE      = regexp.MustCompile(`(\d+)`)
	failedRE      = regexp.MustCompile(`'([^']+)'`)
)

// ParseLeader picks the leader node out of a "leader" detail value
// formatted as either an Erlang map `#{ ..., leader => 'node', ... }`
// or a tuple `{store, 'node'}`. Returns "" if neither shape matches.
func ParseLeader(s string) string {
	if m := leaderRE.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	if m := leaderTupleRE.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	return ""
}

// ParseFirstInt — first integer in s, or 0. Used for term + lag
// fields that come back as Erlang lists / counters.
func ParseFirstInt(s string) int64 {
	if m := maxLagRE.FindStringSubmatch(s); len(m) == 2 {
		var n int64
		for _, c := range m[1] {
			n = n*10 + int64(c-'0')
		}
		return n
	}
	return 0
}

// ParseFailedNodes — all 'node' literals inside an Erlang list.
// Returns nil if the list is empty.
func ParseFailedNodes(s string) []string {
	var out []string
	for _, m := range failedRE.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}
