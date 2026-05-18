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
	"codeberg.org/reckon-db-org/reckon-go/snapshots"
	"codeberg.org/reckon-db-org/reckon-go/stores"
	"codeberg.org/reckon-db-org/reckon-go/streams"
	"codeberg.org/reckon-db-org/reckon-go/subscriptions"
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
	modeStores modeIdx = iota
	modeStreams
	modeSubscriptions
	modeSnapshots
)

var modeLabels = []string{"stores", "streams", "subscriptions", "snapshots"}

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
	stores  *modes.StoresView

	width, height int

	clock time.Time

	// `?' help overlay state.
	showHelp bool

	// Command bar: opens on `/' (filter) or `:' (goto).
	cmdMode cmdMode
	cmdBuf  string
	cmdMsg  string // transient feedback ("no match", "filtering: …")
}

type cmdMode int

const (
	cmdNone cmdMode = iota
	cmdFilter
	cmdGoto
)

func initialModel(endpoint string, c *reckon.Client) *model {
	topo := cluster.New()
	m := &model{
		endpoint:    endpoint,
		client:      c,
		topology:    topo,
		activeStore: "default_store",
		mode:        modeStores,
		clock:       time.Now(),
	}
	m.streams = modes.BuildStreams(c, m.activeStore)
	m.subs = modes.BuildSubscriptions(c, m.activeStore)
	m.snaps = modes.BuildSnapshots(c, m.activeStore)
	m.stores = modes.BuildStores(c, topo, m.activeStore, m.setActiveStore)
	return m
}

// setActiveStore is the callback the cluster mode's stores column
// calls when the user picks a different store. Streams/subs/snaps
// stay bound to whatever store they were built with — we only
// rebuild on an explicit jumpToStreams (which user-selects this
// store as the working set, not just hovers over it in cluster).
func (m *model) setActiveStore(store string) {
	m.activeStore = store
}

// activeRanger returns the *Ranger for the modes that are simple
// rangers. Cluster mode is special (composite of two rangers); for
// it, handleKey/Update/View route directly through m.stores.
func (m *model) activeRanger() *ranger.Ranger {
	switch m.mode {
	case modeSubscriptions:
		return m.subs.Ranger
	case modeSnapshots:
		return m.snaps.Ranger
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
	case modeStores:
		m.stores.SyncDetail()
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
		m.stores.Init(),
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
		if m.mode == modeStores {
			probe = m.stores.HealthProbeCmd()
		}
		return m, tea.Batch(m.watchStoresPollCmd(m2.events_ch, m2.errs_ch), probe)

	case editor.DoneMsg:
		// Nothing to do — bubbletea has already restored the altscreen.
		return m, nil
	}

	// Fan the message through the active mode. Cluster has its own
	// Update (it owns a 4-pane composite, not a single ranger).
	var cmd tea.Cmd
	if m.mode == modeStores {
		cmd = m.stores.Update(msg)
	} else {
		cmd = m.activeRanger().Update(msg)
	}
	m.syncDetail()
	return m, cmd
}

func (m *model) handleKey(key string) (tea.Model, tea.Cmd) {
	// When the help overlay is open, ANY key dismisses it.
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	// Command bar swallows input while open.
	if m.cmdMode != cmdNone {
		return m.handleCmdKey(key)
	}

	switch key {
	case "q", "ctrl+c":
		m.shutdown()
		return m, tea.Quit

	case "?":
		m.showHelp = true
		return m, nil

	case "/":
		m.cmdMode = cmdFilter
		m.cmdBuf = ""
		m.cmdMsg = ""
		// Apply empty filter so any previous one clears as the user starts typing.
		if rg := m.activeRangerInclStores(); rg != nil {
			_ = rg.SetFilter("")
		}
		return m, nil

	case ":":
		m.cmdMode = cmdGoto
		m.cmdBuf = ""
		m.cmdMsg = ""
		return m, nil

	case "1":
		m.mode = modeStores
		return m, m.stores.HealthProbeCmd()
	case "2":
		m.mode = modeStreams
		return m, nil
	case "3":
		m.mode = modeSubscriptions
		return m, nil
	case "4":
		m.mode = modeSnapshots
		return m, nil

	case "enter":
		// In cluster mode with the stores ranger focused on its
		// list column, enter is "open this store in streams mode".
		// Elsewhere enter falls through to ranger semantics (= l).
		if m.mode == modeStores &&
			m.stores.IsStoresFocused() &&
			m.stores.SelectedStore() != "" {
			return m, m.jumpToStreams(m.stores.SelectedStore())
		}

	case "e":
		return m, m.editSelected()

	case "r":
		return m, m.refresh()
	}

	// Delegate the rest to the active mode.
	var cmd tea.Cmd
	if m.mode == modeStores {
		cmd, _ = m.stores.HandleKey(key)
	} else {
		cmd, _ = m.activeRanger().HandleKey(key)
	}
	m.syncDetail()
	return m, cmd
}

// activeRangerInclStores returns the focused ranger for the current
// mode. For modeStores, it returns whichever inner ranger has focus
// (top = nodes, bottom = stores) so filter/goto operate on the
// visually-focused list.
func (m *model) activeRangerInclStores() *ranger.Ranger {
	if m.mode == modeStores {
		return m.stores.FocusedRanger()
	}
	return m.activeRanger()
}

// handleCmdKey runs while the command bar (filter or goto) is open.
// Esc cancels and clears any in-progress filter. Enter commits.
// Backspace edits. Printable characters append to cmdBuf.
func (m *model) handleCmdKey(key string) (tea.Model, tea.Cmd) {
	rg := m.activeRangerInclStores()
	switch key {
	case "esc", "ctrl+c":
		if m.cmdMode == cmdFilter && rg != nil {
			_ = rg.SetFilter("")
			m.syncDetail()
		}
		m.cmdMode = cmdNone
		m.cmdBuf = ""
		m.cmdMsg = ""
		return m, nil

	case "enter":
		mode := m.cmdMode
		buf := m.cmdBuf
		m.cmdMode = cmdNone
		m.cmdBuf = ""
		if mode == cmdGoto && rg != nil && buf != "" {
			if _, hit := rg.GotoID(buf); !hit {
				m.cmdMsg = "no match for: " + buf
			} else {
				m.cmdMsg = ""
			}
			m.syncDetail()
		}
		return m, nil

	case "backspace":
		if len(m.cmdBuf) > 0 {
			m.cmdBuf = m.cmdBuf[:len(m.cmdBuf)-1]
		}
		if m.cmdMode == cmdFilter && rg != nil {
			_ = rg.SetFilter(m.cmdBuf)
			m.syncDetail()
		}
		return m, nil
	}

	// Printable input: append to buffer, live-apply for filter mode.
	if isPrintable(key) {
		m.cmdBuf += key
		if m.cmdMode == cmdFilter && rg != nil {
			_ = rg.SetFilter(m.cmdBuf)
			m.syncDetail()
		}
	}
	return m, nil
}

func isPrintable(k string) bool {
	// bubbletea reports single-character keys as their literal value
	// (e.g. "a", "/", " "), and named keys as words (e.g. "tab").
	// Treat single-rune entries as printable.
	if len(k) == 1 {
		c := k[0]
		return c >= 0x20 && c < 0x7f
	}
	return false
}

// jumpToStreams rebinds the streams view to store, switches mode,
// and returns the new view's Init() so the first fetch fires
// immediately. Existing streams goroutines are released via Stop().
func (m *model) jumpToStreams(store string) tea.Cmd {
	if store != m.activeStore {
		m.activeStore = store
		m.streams.Ranger.Stop()
		m.streams = modes.BuildStreams(m.client, store)
	}
	m.mode = modeStreams
	return m.streams.Ranger.Init()
}

// refresh re-fetches the active mode's primary list. For cluster
// mode that's a fresh HealthService probe (topology updates live
// via the WatchStores stream so doesn't need re-pulling).
func (m *model) refresh() tea.Cmd {
	switch m.mode {
	case modeStreams:
		return m.streams.Refresh()
	case modeSubscriptions:
		return m.subs.Refresh()
	case modeSnapshots:
		return m.snaps.Refresh()
	case modeStores:
		return m.stores.HealthProbeCmd()
	}
	return nil
}

func (m *model) editSelected() tea.Cmd {
	switch m.mode {
	case modeStreams:
		ev, ok := m.streams.SelectedEvent()
		if !ok {
			return nil
		}
		return editor.Inspect(
			fmt.Sprintf("%s_v%d", ev.StreamID, ev.Version),
			"json",
			buildEditorPayload(ev),
		)

	case modeSubscriptions:
		info, ok := m.subs.SelectedSubscription()
		if !ok {
			return nil
		}
		return editor.Inspect(
			"subscription_"+info.Name,
			"json",
			buildSubPayload(info),
		)

	case modeSnapshots:
		rec, ok := m.snaps.SelectedSnapshot()
		if !ok {
			return nil
		}
		return editor.Inspect(
			fmt.Sprintf("snap_%s_v%d", rec.StreamID, rec.Version),
			"json",
			buildSnapshotPayload(rec),
		)
	}
	return nil
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

func buildSubPayload(in subscriptions.Info) []byte {
	envelope := map[string]any{
		"id":         in.ID,
		"name":       in.Name,
		"type":       string(in.Type),
		"selector":   in.Selector,
		"pool_size":  in.PoolSize,
		"checkpoint": in.Checkpoint,
		"created_at": in.CreatedAt.Format(time.RFC3339),
	}
	out, _ := json.MarshalIndent(envelope, "", "  ")
	return out
}

func buildSnapshotPayload(r snapshots.Record) []byte {
	envelope := map[string]any{
		"stream_id": r.StreamID,
		"version":   r.Version,
		"timestamp": r.Timestamp.Format(time.RFC3339),
		"data":      decodeJSONOrRaw(r.Data),
	}
	if len(r.Metadata) > 0 {
		envelope["metadata"] = decodeJSONOrRaw(r.Metadata)
	}
	if len(r.AnchorHash) > 0 {
		envelope["anchor_hash_hex"] = fmt.Sprintf("%x", r.AnchorHash)
	}
	out, _ := json.MarshalIndent(envelope, "", "  ")
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

	// Chrome consumes 3 lines: header (1) + modeBar (1) + statusBar (1).
	// Body fills the remaining height exactly so the frame matches the
	// terminal — leaving even a one-line gap causes bubbletea's altscreen
	// to retain a stale status bar from the previous frame.
	bodyH := h - 3
	var body string
	if m.mode == modeStores {
		body = m.stores.View(w, bodyH)
	} else {
		body = m.activeRanger().View(w, bodyH)
	}

	hints := []ui.KeyHint{
		{Key: "j/k", Action: "move"},
		{Key: "h/l", Action: "in/out"},
	}
	if m.mode == modeStores {
		hints = append(hints, ui.KeyHint{Key: "tab", Action: "swap rangers"})
	}
	hints = append(hints,
		ui.KeyHint{Key: "1-4", Action: "mode"},
		ui.KeyHint{Key: "/", Action: "filter"},
		ui.KeyHint{Key: ":", Action: "goto"},
		ui.KeyHint{Key: "e", Action: "edit"},
		ui.KeyHint{Key: "r", Action: "refresh"},
		ui.KeyHint{Key: "?", Action: "help"},
		ui.KeyHint{Key: "q", Action: "quit"},
	)
	summary := m.statusSummary()
	status := ui.StatusBar(hints, summary, w)

	frame := lipgloss.JoinVertical(lipgloss.Left,
		header,
		modeBar,
		body,
		status,
	)

	if m.showHelp {
		return ui.HelpOverlay(modeLabels[int(m.mode)], helpFor(m.mode), w, h)
	}
	return frame
}

// statusSummary returns the right-aligned text for the status bar.
// When the command bar is open it shows the prompt + buffer; when a
// transient message is set it shows that; otherwise the clock.
func (m *model) statusSummary() string {
	switch {
	case m.cmdMode == cmdFilter:
		return "/" + m.cmdBuf + "▍"
	case m.cmdMode == cmdGoto:
		return ":goto " + m.cmdBuf + "▍"
	case m.cmdMsg != "":
		return m.cmdMsg
	default:
		return fmt.Sprintf("%s · %s", "◉", m.clock.Format("15:04:05"))
	}
}

// helpFor returns the cheatsheet sections for the active mode.
// Shared global keys come first, then mode-specific.
func helpFor(mode modeIdx) []ui.HelpSection {
	global := ui.HelpSection{
		Title: "global",
		Bindings: []ui.HelpBinding{
			{Keys: "1 / 2 / 3 / 4", What: "switch mode (cluster / streams / subs / snaps)"},
			{Keys: "?", What: "toggle this help"},
			{Keys: "q / ctrl+c", What: "quit"},
		},
	}
	nav := ui.HelpSection{
		Title: "navigation",
		Bindings: []ui.HelpBinding{
			{Keys: "j / k / ↓ / ↑", What: "move within focused column"},
			{Keys: "h / l / ← / →", What: "ascend / descend within ranger"},
			{Keys: "g / G", What: "jump to top / bottom"},
			{Keys: "/", What: "filter focused column (case-insensitive substring; esc clears)"},
			{Keys: ":", What: "jump to a specific id in focused column (case-insensitive substring)"},
		},
	}
	actions := ui.HelpSection{
		Title: "actions",
		Bindings: []ui.HelpBinding{
			{Keys: "r", What: "refresh current mode's primary list"},
			{Keys: "e", What: "open selected leaf in $EDITOR (read-only)"},
		},
	}
	switch mode {
	case modeStores:
		return []ui.HelpSection{
			global, nav, actions,
			{Title: "stores (4-pane grid)", Bindings: []ui.HelpBinding{
				{Keys: "tab", What: "swap focus between top (nodes) and bottom (stores) rangers"},
				{Keys: "enter", What: "open selected store in streams mode (when stores list is focused)"},
				{Keys: "j/k on stores", What: "switch active store; everything follows"},
			}},
		}
	case modeStreams:
		return []ui.HelpSection{
			global, nav, actions,
			{Title: "streams", Bindings: []ui.HelpBinding{
				{Keys: "enter / l", What: "descend column (streams → events → detail)"},
				{Keys: "e", What: "open event envelope + data + metadata in $EDITOR"},
			}},
		}
	case modeSubscriptions:
		return []ui.HelpSection{
			global, nav, actions,
			{Title: "subscriptions", Bindings: []ui.HelpBinding{
				{Keys: "e", What: "open subscription Info struct in $EDITOR"},
			}},
		}
	case modeSnapshots:
		return []ui.HelpSection{
			global, nav, actions,
			{Title: "snapshots", Bindings: []ui.HelpBinding{
				{Keys: "e", What: "open snapshot data + metadata in $EDITOR"},
			}},
		}
	}
	return []ui.HelpSection{global, nav, actions}
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
	m.stores.Stop()
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
