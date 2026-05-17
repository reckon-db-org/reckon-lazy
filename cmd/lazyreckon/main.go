// lazyreckon — a terminal UI for the ReckonDB event store, in the
// spirit of lazygit / lazydocker / k9s.
//
// Layout: ranger-style three-column miller view. Bottom mode strip
// swaps what the columns list (streams / subscriptions / snapshots).
// Header carries the active store + cluster health.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-go/stores"
	"codeberg.org/reckon-db-org/reckon-go/streams"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/cluster"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/editor"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/modes"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/profiles"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ranger"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/splash"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/ui"
)

type modeIdx int

const (
	modeStreams modeIdx = iota
	modeSubscriptions
	modeSnapshots
	modeCluster
)

var modeLabels = []string{"streams", "subscriptions", "snapshots", "cluster"}

type model struct {
	endpoint string
	client   *reckon.Client

	// Shared cluster topology — fed by the top-level WatchStores
	// poller, read by the header + the cluster mode.
	topology    *cluster.Topology
	activeStore string

	// Mode + ranger.
	mode    modeIdx
	streams *modes.StreamsView
	subs    *modes.SubscriptionsView
	snaps   *modes.SnapshotsView
	cluster *modes.ClusterView

	width, height int

	clock time.Time
}

func initialModel(endpoint string, c *reckon.Client) *model {
	topo := cluster.New()
	m := &model{
		endpoint:    endpoint,
		client:      c,
		topology:    topo,
		activeStore: "default_store",
		mode:        modeStreams,
		clock:       time.Now(),
	}
	m.streams = modes.BuildStreams(c, m.activeStore)
	m.subs = modes.BuildSubscriptions(c, m.activeStore)
	m.snaps = modes.BuildSnapshots(c, m.activeStore)
	m.cluster = modes.BuildCluster(c, topo)
	return m
}

func (m *model) activeRanger() *ranger.Ranger {
	switch m.mode {
	case modeSubscriptions:
		return m.subs.Ranger
	case modeSnapshots:
		return m.snaps.Ranger
	case modeCluster:
		return m.cluster.Ranger
	default:
		return m.streams.Ranger
	}
}

func (m *model) syncDetail() {
	switch m.mode {
	case modeStreams:
		m.streams.SyncDetail()
	case modeSubscriptions:
		m.subs.SyncDetail()
	case modeSnapshots:
		m.snaps.SyncDetail()
	case modeCluster:
		m.cluster.SyncDetail()
	}
}

//------------------------------------------------------------------------------
// Init

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
		m.watchStoresCmd(),
		modes.HealthTick(),
		m.streams.Ranger.Init(),
		m.subs.Ranger.Init(),
		m.snaps.Ranger.Init(),
		m.cluster.Ranger.Init(),
	}
	return tea.Batch(cmds...)
}

//------------------------------------------------------------------------------
// Update

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m2 := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = m2.Width, m2.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(m2.String())

	case tickMsg:
		m.clock = time.Time(m2)
		return m, tickCmd()

	case storesTickMsg:
		for _, ev := range m2.events {
			m.topology.ApplyEvent(ev)
		}
		if m2.err != nil {
			m.topology.SetError(m2.err)
		}
		// Re-arm and, if we're in cluster mode, kick a probe for
		// the selected store now that topology may have new nodes.
		probe := tea.Cmd(nil)
		if m.mode == modeCluster {
			probe = m.cluster.HealthProbeCmd()
		}
		return m, tea.Batch(m.watchStoresPollCmd(m2.events_ch, m2.errs_ch), probe)

	case editor.DoneMsg:
		// Nothing to do — bubbletea has already restored the altscreen.
		return m, nil
	}

	// Fan the message through the active ranger only — non-active
	// modes don't need wakeups for raw key/tick messages.
	cmd := m.activeRanger().Update(msg)
	m.syncDetail()
	return m, cmd
}

func (m *model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		m.shutdown()
		return m, tea.Quit

	case "1":
		m.mode = modeStreams
		return m, nil
	case "2":
		m.mode = modeSubscriptions
		return m, nil
	case "3":
		m.mode = modeSnapshots
		return m, nil
	case "4":
		m.mode = modeCluster
		return m, m.cluster.HealthProbeCmd()

	case "e":
		return m, m.editSelected()
	}

	// Delegate the rest to the ranger.
	cmd, _ := m.activeRanger().HandleKey(key)
	m.syncDetail()
	return m, cmd
}

func (m *model) editSelected() tea.Cmd {
	if m.mode != modeStreams {
		return nil
	}
	ev, ok := m.streams.SelectedEvent()
	if !ok {
		return nil
	}
	payload := buildEditorPayload(ev)
	name := fmt.Sprintf("%s_v%d", ev.StreamID, ev.Version)
	return editor.Inspect(name, "json", payload)
}

// buildEditorPayload combines envelope + data + metadata into one
// readable JSON document for the editor.
func buildEditorPayload(ev streams.RecordedEvent) []byte {
	envelope := map[string]any{
		"event_id":   ev.EventID,
		"event_type": ev.EventType,
		"stream_id":  ev.StreamID,
		"version":    ev.Version,
		"timestamp":  ev.Timestamp.Format(time.RFC3339),
		"tags":       ev.Tags,
	}
	envelope["data"] = decodeJSONOrRaw(ev.Data)
	if len(ev.Metadata) > 0 {
		envelope["metadata"] = decodeJSONOrRaw(ev.Metadata)
	}
	out, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return []byte(fmt.Sprintf("marshal failed: %v\n\nraw data: %s", err, ev.Data))
	}
	return out
}

func decodeJSONOrRaw(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return v
	}
	return string(raw)
}

//------------------------------------------------------------------------------
// View

func (m *model) View() string {
	w := m.width
	if w < 40 {
		w = 80
	}
	h := m.height
	if h < 10 {
		h = 24
	}

	health := m.deriveHealth()
	header := ui.Header(m.endpoint, m.activeStore, health, w)
	modeBar := ui.ModeStrip(modeLabels, int(m.mode), w)

	bodyH := h - 4 // header + modebar + statusbar + 1 padding line
	body := m.activeRanger().View(w, bodyH)

	hints := []ui.KeyHint{
		{Key: "j/k", Action: "move"},
		{Key: "h/l", Action: "in/out"},
		{Key: "1-4", Action: "mode"},
		{Key: "e", Action: "edit"},
		{Key: "q", Action: "quit"},
	}
	summary := fmt.Sprintf("%s · %s", "◉", m.clock.Format("15:04:05"))
	status := ui.StatusBar(hints, summary, w)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		modeBar,
		body,
		status,
	)
}

func (m *model) deriveHealth() ui.Health {
	nodes := map[string]bool{}
	for _, inst := range m.topology.NodesFor(m.activeStore) {
		nodes[inst.Node] = true
	}
	total := len(nodes)
	hp := m.topology.Health(m.activeStore)
	out := ui.Health{
		NodesUp:    total,
		NodesTotal: total,
		OK:         total > 0 && m.topology.Err() == nil,
		Leader:     hp.Leader,
	}
	// If we have a real probe result, prefer its node counts.
	if !hp.LastProbed.IsZero() && hp.NodesTotal > 0 {
		out.NodesUp = hp.NodesUp
		out.NodesTotal = hp.NodesTotal
		out.OK = hp.OK
	}
	return out
}

//------------------------------------------------------------------------------
// Stores watch — top-level, drives header

type storesTickMsg struct {
	events     []stores.Event
	err        error
	events_ch  <-chan stores.Event
	errs_ch    <-chan error
}

func (m *model) watchStoresCmd() tea.Cmd {
	ctx := context.Background() // lives for app lifetime
	events, errs := m.client.Stores().Watch(ctx, stores.WithSnapshot(true))
	return m.watchStoresPollCmd(events, errs)
}

func (m *model) watchStoresPollCmd(events <-chan stores.Event, errs <-chan error) tea.Cmd {
	return func() tea.Msg {
		// Drain at least one event, then any others that are
		// immediately available, then return as a batch. Keeps
		// the message rate bounded under churn.
		ev, ok := <-events
		if !ok {
			err := <-errs
			return storesTickMsg{err: err, events_ch: events, errs_ch: errs}
		}
		batch := []stores.Event{ev}
	drain:
		for {
			select {
			case e, ok := <-events:
				if !ok {
					break drain
				}
				batch = append(batch, e)
			default:
				break drain
			}
		}
		sort.SliceStable(batch, func(i, j int) bool {
			return batch[i].At.Before(batch[j].At)
		})
		return storesTickMsg{events: batch, events_ch: events, errs_ch: errs}
	}
}

//------------------------------------------------------------------------------
// Clock tick

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

//------------------------------------------------------------------------------

func (m *model) shutdown() {
	m.streams.Ranger.Stop()
	m.subs.Ranger.Stop()
	m.snaps.Ranger.Stop()
	m.cluster.Ranger.Stop()
	_ = m.client.Close()
}

func main() {
	endpointFlag := flag.String("endpoint", "",
		"reckon-gateway gRPC endpoint (host:port) — skips the splash")
	profileFlag := flag.String("profile", "",
		"saved profile name from profiles.toml — skips the splash")
	saveAs := flag.String("save-as", "",
		"save --endpoint as a new profile under this name (also connects)")
	flag.Parse()

	if err := run(*endpointFlag, *profileFlag, *saveAs); err != nil {
		fmt.Fprintf(os.Stderr, "lazyreckon: %v\n", err)
		os.Exit(1)
	}
}

func run(endpointFlag, profileFlag, saveAs string) error {
	store, err := loadProfiles()
	if err != nil {
		return fmt.Errorf("load profiles: %w", err)
	}

	endpoint, profileName, err := resolveEndpoint(store, endpointFlag, profileFlag, saveAs)
	if err != nil {
		return err
	}
	if endpoint == "" {
		// User cancelled the splash. Exit cleanly.
		return nil
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := reckon.Connect(dialCtx, endpoint)
	if err != nil {
		return fmt.Errorf("connect %s: %w", endpoint, err)
	}

	// Touch + save the chosen profile so it sorts to the top next
	// time. Touch errors are non-fatal; we still proceed into the
	// TUI.
	if profileName != "" {
		if err := store.Touch(profileName); err == nil {
			_ = store.Save()
		}
	}

	p := tea.NewProgram(initialModel(endpoint, c), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// resolveEndpoint picks one of:
//   - --endpoint flag (optionally --save-as adds it to the store)
//   - --profile flag
//   - splash picker (interactive)
//
// Returns ("", "", nil) when the user cancels the picker.
func resolveEndpoint(store *profiles.Store, endpointFlag, profileFlag, saveAs string) (endpoint, name string, err error) {
	switch {
	case endpointFlag != "":
		if saveAs != "" {
			if err := profiles.ValidateName(saveAs); err != nil {
				return "", "", fmt.Errorf("--save-as: %w", err)
			}
			if err := profiles.ValidateEndpoint(endpointFlag); err != nil {
				return "", "", fmt.Errorf("--endpoint: %w", err)
			}
			if err := store.Add(profiles.Profile{Name: saveAs, Endpoint: endpointFlag}); err != nil {
				return "", "", fmt.Errorf("save profile: %w", err)
			}
			if err := store.Save(); err != nil {
				return "", "", fmt.Errorf("save profiles: %w", err)
			}
			return endpointFlag, saveAs, nil
		}
		return endpointFlag, "", nil

	case profileFlag != "":
		p, err := store.Find(profileFlag)
		if err != nil {
			return "", "", fmt.Errorf("profile %q: %w", profileFlag, err)
		}
		return p.Endpoint, p.Name, nil

	default:
		return runSplash(store)
	}
}

func runSplash(store *profiles.Store) (endpoint, name string, err error) {
	sp := splash.New(store)
	p := tea.NewProgram(sp, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return "", "", err
	}
	r := sp.Result()
	if r.Cancelled || r.Selected == nil {
		return "", "", nil
	}
	return r.Selected.Endpoint, r.Selected.Name, nil
}

func loadProfiles() (*profiles.Store, error) {
	path, err := profiles.DefaultPath()
	if err != nil {
		return nil, err
	}
	return profiles.Load(path)
}
