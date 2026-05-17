// lazyreckon — a terminal UI for the ReckonDB event store, in the
// spirit of lazygit / lazydocker / k9s.
//
// Repo: codeberg.org/reckon-db-org/reckon-lazy (binary stays
// `lazyreckon` to fit the lazy-* family).
//
// Status: scaffold. Bubble Tea entry point that prints the planned
// pane layout. Panes ship incrementally once the reckon-go SDK has
// the underlying service wrappers.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	endpoint string
}

func initialModel(endpoint string) model {
	return model{endpoint: endpoint}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	return fmt.Sprintf(`
  lazyreckon — scaffold

  endpoint: %s

  planned panes (none implemented yet):
    [1] cluster   — nodes, leader, raft membership
    [2] stores    — discovered store instances (StoresService.WatchStores)
    [3] streams   — per-store stream list
    [4] events    — tail a stream live (SubscriptionService.Subscribe)
    [5] subscriptions — checkpoints + lag

  q  quit

`, m.endpoint)
}

func main() {
	endpoint := flag.String("endpoint", "localhost:50051",
		"reckon-gateway gRPC endpoint (host:port)")
	flag.Parse()

	p := tea.NewProgram(initialModel(*endpoint), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "lazyreckon: %v\n", err)
		os.Exit(1)
	}
}
