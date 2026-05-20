package cluster

import (
	"reflect"
	"testing"

	"codeberg.org/reckon-db-org/reckon-go/stores"
)

// --- Topology aggregation ------------------------------------------

func inst(storeID, node string) stores.Instance {
	return stores.Instance{StoreID: storeID, Node: node, Mode: stores.ModeSingle}
}

func announced(storeID, node string) stores.Event {
	return stores.Event{Type: stores.EventAnnounced, Instance: inst(storeID, node)}
}

func retired(storeID, node string) stores.Event {
	return stores.Event{Type: stores.EventRetired, Instance: inst(storeID, node)}
}

func TestTopology_Empty(t *testing.T) {
	top := New()
	if got := top.Stores(); len(got) != 0 {
		t.Errorf("new topology should have no stores, got %v", got)
	}
	if top.NodeCount() != 0 {
		t.Errorf("new topology NodeCount = %d, want 0", top.NodeCount())
	}
}

func TestTopology_ApplyAnnouncedAndList(t *testing.T) {
	top := New()
	top.ApplyEvent(announced("parksim_lot_store", "n1@h"))
	top.ApplyEvent(announced("parksim_lot_store", "n2@h"))
	top.ApplyEvent(announced("parksim_pricing_store", "n3@h"))

	wantStores := []string{"parksim_lot_store", "parksim_pricing_store"} // sorted
	if got := top.Stores(); !reflect.DeepEqual(got, wantStores) {
		t.Errorf("Stores() = %v, want %v", got, wantStores)
	}
	if top.NodeCount() != 3 {
		t.Errorf("NodeCount = %d, want 3", top.NodeCount())
	}
	if top.NodeCountFor("parksim_lot_store") != 2 {
		t.Errorf("NodeCountFor(lot) = %d, want 2", top.NodeCountFor("parksim_lot_store"))
	}
	if top.Events() != 3 {
		t.Errorf("Events() = %d, want 3", top.Events())
	}
}

func TestTopology_RetiredRemoves(t *testing.T) {
	top := New()
	top.ApplyEvent(announced("s", "n1@h"))
	top.ApplyEvent(announced("s", "n2@h"))
	top.ApplyEvent(retired("s", "n1@h"))

	if top.NodeCountFor("s") != 1 {
		t.Errorf("after retire, NodeCountFor(s) = %d, want 1", top.NodeCountFor("s"))
	}
	nodes := top.NodesFor("s")
	if len(nodes) != 1 || nodes[0].Node != "n2@h" {
		t.Errorf("NodesFor(s) = %+v, want [n2@h]", nodes)
	}
}

func TestTopology_NodesForSorted(t *testing.T) {
	top := New()
	top.ApplyEvent(announced("s", "n3@h"))
	top.ApplyEvent(announced("s", "n1@h"))
	top.ApplyEvent(announced("s", "n2@h"))
	nodes := top.NodesFor("s")
	got := []string{nodes[0].Node, nodes[1].Node, nodes[2].Node}
	want := []string{"n1@h", "n2@h", "n3@h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NodesFor sorted = %v, want %v", got, want)
	}
}

func TestTopology_HealthRoundTrip(t *testing.T) {
	top := New()
	h := StoreHealth{Leader: "n1@h", Term: 7, HasQuorum: true, OK: true, Status: "healthy"}
	top.PutHealth("s", h)
	if got := top.Health("s"); got.Leader != "n1@h" || got.Term != 7 || !got.OK {
		t.Errorf("Health(s) = %+v", got)
	}
	if got := top.Health("missing"); got.Status != "" {
		t.Errorf("Health(missing) should be zero, got %+v", got)
	}
}

func TestTopology_Error(t *testing.T) {
	top := New()
	if top.Err() != nil {
		t.Errorf("new topology Err() = %v, want nil", top.Err())
	}
	top.SetError(errFake)
	if top.Err() != errFake {
		t.Errorf("Err() = %v, want errFake", top.Err())
	}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "boom" }

// --- Erlang-detail parsers -----------------------------------------

func TestParseLeader(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"map form", `#{term => 4, leader => 'reckon_gateway@192.168.1.12', quorum => true}`, "reckon_gateway@192.168.1.12"},
		{"tuple form", `{parksim_lot_store,'reckon_gateway@192.168.1.11'}`, "reckon_gateway@192.168.1.11"},
		{"no leader", `#{quorum => false}`, ""},
		{"empty", ``, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseLeader(tc.in); got != tc.want {
				t.Errorf("ParseLeader(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseFirstInt(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{`[42]`, 42},
		{`term => 7`, 7},
		{`lag: 1234 events`, 1234},
		{`no digits here`, 0},
		{``, 0},
		{`abc123def456`, 123},
	}
	for _, tc := range tests {
		if got := ParseFirstInt(tc.in); got != tc.want {
			t.Errorf("ParseFirstInt(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseFailedNodes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"two nodes", `['n1@h','n2@h']`, []string{"n1@h", "n2@h"}},
		{"one node", `['only@h']`, []string{"only@h"}},
		{"empty list", `[]`, nil},
		{"no quotes", `no nodes`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseFailedNodes(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseFailedNodes(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
