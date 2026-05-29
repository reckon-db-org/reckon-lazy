package modes

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"codeberg.org/reckon-db-org/reckon-go/stores"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/cluster"
)

// fourTenantTopo mirrors the live parksim fleet: four single-mode
// stores, one node each.
func fourTenantTopo() *cluster.Topology {
	topo := cluster.New()
	for _, s := range []struct{ store, node string }{
		{"parksim_leuven_store", "parksim@192.168.1.10"},
		{"parksim_brussels_store", "parksim@192.168.1.11"},
		{"parksim_ghent_store", "parksim@192.168.1.12"},
		{"parksim_antwerp_store", "parksim@192.168.1.13"},
	} {
		topo.ApplyEvent(stores.Event{
			Type: stores.EventAnnounced,
			Instance: stores.Instance{
				StoreID:      s.store,
				Node:         s.node,
				Mode:         stores.ModeSingle,
				DataDir:      "/var/lib/hecate-parksim/" + s.store,
				RegisteredAt: time.Unix(1780000000, 0),
			},
		})
	}
	return topo
}

// TestStoresViewFitsBudget guards the doubled/stacked-frame bug: the
// stores-mode body MUST render exactly the height it's handed, and no
// row may exceed the width. A line wider than w wraps in the terminal
// and pushes the frame past the bottom, scrolling the status bar into
// a stale duplicate; more than h lines does the same vertically.
func TestStoresViewFitsBudget(t *testing.T) {
	topo := fourTenantTopo()
	v := BuildStores(nil, topo, "parksim_leuven_store", func(string) {})
	v.SyncDetail()

	// A spread of realistic terminal geometries (incl. the full-HD
	// 187x44-ish from the bug report).
	for _, dim := range []struct{ w, h int }{
		{187, 44}, {120, 30}, {100, 24}, {80, 20}, {200, 50},
	} {
		out := v.View(dim.w, dim.h)
		lines := strings.Split(out, "\n")

		if len(lines) != dim.h {
			t.Errorf("w=%d h=%d: body rendered %d lines, want exactly %d",
				dim.w, dim.h, len(lines), dim.h)
		}
		for i, ln := range lines {
			if got := lipgloss.Width(ln); got > dim.w {
				t.Errorf("w=%d h=%d: row %d width %d exceeds %d: %q",
					dim.w, dim.h, i, got, dim.w, ln)
			}
		}
	}
}
