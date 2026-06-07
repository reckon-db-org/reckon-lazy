package main

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	reckon "codeberg.org/reckon-db-org/reckon-go"
)

// newTestModel builds a real model against a lazy (never-dialed) gRPC
// client. grpc.NewClient is non-blocking, so this needs no live
// gateway; key handling never triggers an RPC because no store is
// active yet (bind*ToActive short-circuit on the empty store).
func newTestModel(t *testing.T) *model {
	t.Helper()
	c, err := reckon.Connect(context.Background(), "passthrough:///127.0.0.1:1")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	m := initialModel("127.0.0.1:1", c)
	m.width, m.height = 120, 40 // past the "suppress first render" guard
	return m
}

// quits reports whether the cmd resolves to tea.QuitMsg.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		m := newTestModel(t)
		_, cmd := m.handleKey(k)
		if !quits(cmd) {
			t.Errorf("key %q did not quit", k)
		}
	}
}

func TestModeSwitchKeys(t *testing.T) {
	m := newTestModel(t)
	for _, tc := range []struct {
		key  string
		want modeIdx
	}{
		{"2", modeStreams},
		{"3", modeSubscriptions},
		{"4", modeSnapshots},
		{"1", modeStores},
	} {
		m.handleKey(tc.key)
		if m.mode != tc.want {
			t.Errorf("key %q: mode = %d, want %d", tc.key, m.mode, tc.want)
		}
	}
}

func TestCommandBarKeys(t *testing.T) {
	m := newTestModel(t)

	m.handleKey("/")
	if m.cmdMode != cmdFilter {
		t.Fatalf("/ did not open filter: cmdMode = %d", m.cmdMode)
	}
	// While the bar is open, q must NOT quit — it types into the buffer.
	if _, cmd := m.handleKey("q"); quits(cmd) {
		t.Error("q quit while the filter bar was open (should type into buffer)")
	}
	if m.cmdBuf != "q" {
		t.Errorf("filter buffer = %q, want \"q\"", m.cmdBuf)
	}
	// Esc closes the bar; q quits again.
	m.handleKey("esc")
	if m.cmdMode != cmdNone {
		t.Fatalf("esc did not close the bar: cmdMode = %d", m.cmdMode)
	}
	if _, cmd := m.handleKey("q"); !quits(cmd) {
		t.Error("q did not quit after the bar closed")
	}

	m = newTestModel(t)
	m.handleKey(":")
	if m.cmdMode != cmdGoto {
		t.Fatalf(": did not open goto: cmdMode = %d", m.cmdMode)
	}
}

func TestHelpOverlayKeys(t *testing.T) {
	m := newTestModel(t)
	m.handleKey("?")
	if !m.showHelp {
		t.Fatal("? did not open help")
	}
	// Any key dismisses help (and does nothing else).
	if _, cmd := m.handleKey("q"); quits(cmd) {
		t.Error("q quit while help was open (should only dismiss help)")
	}
	if m.showHelp {
		t.Error("help still open after a keypress")
	}
}

func TestDrillNavKeysReachActiveDrill(t *testing.T) {
	m := newTestModel(t)
	m.handleKey("2") // streams
	// l / h / j / k must be handled by the active drill, not swallowed.
	for _, k := range []string{"l", "h", "j", "k", "g", "G"} {
		// Should not panic and should leave us in streams mode.
		m.handleKey(k)
		if m.mode != modeStreams {
			t.Fatalf("key %q changed mode to %d", k, m.mode)
		}
	}
}
