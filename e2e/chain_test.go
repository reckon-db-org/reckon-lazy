//go:build e2e

// Package e2e drives the full lazyreckon data path against a LIVE
// reckon-gateway: gateway (gRPC + catalogue) -> reckon-go SDK ->
// lazyreckon's cluster.Topology aggregation.
//
// Gated behind the `e2e` build tag; not part of `go test ./...`:
//
//	RECKON_E2E_ENDPOINT=beam01.lab:50051 go test -tags e2e ./e2e/...
//
// This is the top of the chain the user cares about: it proves that
// what the gateway announces over WatchStores is what lazyreckon's
// Topology ends up holding — the exact data the stores-mode view
// renders.
package e2e

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/stores"

	"codeberg.org/reckon-db-org/reckon-lazy/internal/cluster"
)

func endpoint() string {
	if e := os.Getenv("RECKON_E2E_ENDPOINT"); e != "" {
		return e
	}
	return "beam01.lab:50051"
}

// grpc-go's dns resolver skips /etc/hosts; pre-resolve lab hostnames.
func resolve(ep string) string {
	host, port, err := net.SplitHostPort(ep)
	if err != nil || net.ParseIP(host) != nil {
		return ep
	}
	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		return ep
	}
	return net.JoinHostPort(ips[0], port)
}

// TestChain_TopologyPopulatesFromGateway opens the same WatchStores
// stream lazyreckon's model uses, feeds each event through the real
// cluster.Topology.ApplyEvent, and asserts the aggregated view
// matches what the gateway reports via the (independent) Stores.List
// snapshot. This is gateway -> reckon-go -> lazyreckon end to end.
func TestChain_TopologyPopulatesFromGateway(t *testing.T) {
	c, err := reckon.Connect(context.Background(), resolve(endpoint()))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// Ground truth: the catalogue snapshot.
	listCtx, listCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer listCancel()
	insts, err := c.Stores().List(listCtx)
	if err != nil {
		t.Fatalf("Stores.List: %v", err)
	}
	if len(insts) == 0 {
		t.Fatal("catalogue empty — nothing to aggregate")
	}
	wantStores := map[string]bool{}
	for _, i := range insts {
		wantStores[i.StoreID] = true
	}

	// Drive lazyreckon's Topology from the WatchStores snapshot.
	top := cluster.New()
	wctx, wcancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer wcancel()
	events, errs := c.Stores().Watch(wctx)

	// Consume the initial-snapshot phase: once we've seen at least as
	// many announced instances as the List returned, we have the full
	// snapshot and can stop.
	seen := 0
	for ev := range events {
		top.ApplyEvent(ev)
		if ev.Type == stores.EventAnnounced {
			seen++
		}
		if seen >= len(insts) {
			wcancel()
		}
	}
	if err := <-errs; err != nil && wctx.Err() == nil {
		t.Fatalf("watch error: %v", err)
	}

	// The aggregated topology must contain every store the catalogue
	// snapshot reported.
	gotStores := map[string]bool{}
	for _, s := range top.Stores() {
		gotStores[s] = true
	}
	for s := range wantStores {
		if !gotStores[s] {
			t.Errorf("Topology missing store %q that Stores.List reported", s)
		}
	}
	t.Logf("Topology aggregated %d stores / %d node-instances from WatchStores",
		len(top.Stores()), top.NodeCount())
	if top.NodeCount() == 0 {
		t.Fatal("Topology has zero node-instances after consuming the snapshot")
	}
}
