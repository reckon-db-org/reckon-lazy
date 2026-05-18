# Changelog

All notable changes to `reckon-lazy` (binary: `lazyreckon`) will be documented in this file.

Format: [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning: [SemVer](https://semver.org/).

## [Unreleased]

## [0.4.0] - 2026-05-18

### Added — `/` filter and `:` goto

Two new keys for navigating long lists without scrolling:

- `/<text>` opens a live, case-insensitive substring filter on the
  focused column. Backspace edits; esc clears the filter and closes
  the bar; enter commits the current filter (and leaves it applied).
- `:<id>` opens a one-shot jump. Enter searches the focused column
  for a row matching the id (case-insensitive substring; in the
  events column it also accepts a bare version like `v42` or `42`).
  Hit jumps and clears any in-progress filter; miss leaves the
  cursor and shows "no match" in the status bar.

Both indicators render in the right side of the status bar while open:

```
[ ... ] /sub_                    ← filter mode while typing
[ ... ] :goto users-abc_         ← goto mode while typing
[ ... ] no match for: orph       ← transient after a missed goto
```

Detail / info / 4-pane composite columns no-op these — they own no
filterable rows. The store-mode top and bottom rangers each accept
filter/goto independently based on which one is focused.

### Added — interface contract: `SetFilter` / `GotoID` on Column

`internal/ranger.Column` gains two methods. Every column type
implements them; detail columns ship no-ops. List-style columns
hold a `visible []int` index slice when a filter is active so that
Move / Selected / View all operate on the filtered subset without
mutating the underlying data.

## [0.3.0] - 2026-05-18

### Changed — mode 1 renamed `cluster` → `stores`, layout swapped (breaking)

Mode 1's primary unit of navigation has always been the **store**
(its bottom-left column listed stores; everything else was derived
from the selected store). Naming it `cluster` was an abstraction;
`stores` matches the data-type-noun pattern of modes 2/3/4
(streams, subscriptions, snapshots) and leaves room for a future
`cluster` mode that addresses the substrate (BEAM mesh, gossip,
discovery — concerns that exist independent of any store).

The 4-pane grid is also reordered so the scope selector lives at
the top and the drilldown at the bottom. Now:

```
┌───────────────────┬──────────────────────┐
│ stores            │ store info           │   ← top (focused at boot)
│ ● default_store   │ status:  healthy     │
│                   │ leader:  192.168.1.10│
│                   │ quorum:  5/5 up      │
├───────────────────┼──────────────────────┤
│ nodes             │ node detail          │   ← bottom
│   192.168.1.10 ★  │ name:    .10         │
│ ▸ 192.168.1.11    │ role:    follower    │
│   192.168.1.100   │ mode:    cluster     │
└───────────────────┴──────────────────────┘
```

Reading top-to-bottom matches selection-then-drill, the first
glance hits the store-info dashboard, and boot focus sits where
the eye naturally starts. The internal `ClusterView`/`BuildCluster`
rename to `StoresView`/`BuildStores`; column types lose their
`cluster*` prefix; the position-based focus check
(`FocusedRanger() == 1`) is replaced by a semantic
`IsStoresFocused()` helper.

`internal/cluster/` package stays — it legitimately holds the
substrate-level Topology + StoreHealth types, reusable by a future
real cluster-substrate mode.

### Other changes

None this release; the rename + swap is the whole thing.

## [0.2.0] - 2026-05-18

First fully-wired release. All four modes (cluster, streams,
subscriptions, snapshots) drive live data; refresh, help, and
per-cursor probing round out the interaction model.

### Added

- **Splash profile picker** as the startup screen.
  `$XDG_CONFIG_HOME/lazyreckon/profiles.toml` stores named
  cluster endpoints. `n` add / `r` rename / `d` delete /
  `t` test (5s `GetServerInfo` ping with green/red badge) /
  enter connect. `--profile NAME` and `--endpoint host:port`
  CLI flags skip the splash; `--save-as NAME` adds-and-connects
  in one shot.
- **Cluster mode** (4-pane grid, mode 1): nodes/detail on top,
  stores/info on bottom. Bottom-left's cursor IS the model's
  active store; moving it syncs `m.activeStore` so streams /
  subs / snaps re-bind to the chosen store. `tab` swaps focus
  between top and bottom rangers. Live cluster banner shows
  status, leader, quorum, term, lag, failed nodes.
- **Subscriptions mode** (mode 3): subs list → lag detail →
  full Info via `SubscriptionService.{List, GetLag, Get}`.
- **Snapshots mode** (mode 4): streams-that-have-snapshots →
  versions (newest first) → data + anchor hash + metadata via
  `SnapshotService.{ListAll, List, At}`.
- **Per-cursor probe in cluster mode**: moving the store
  cursor fires `HealthProbeCmd` immediately, no waiting for
  the 5s tick.
- **`r` refresh** key across all four modes (streams/subs/snaps
  re-fetch col 0; cluster re-probes the HealthService).
- **`?` help overlay**: mode-aware cheatsheet modal. Press
  anything to dismiss. Replaces "remember the keys or read the
  README".
- **`enter` on a store in cluster** opens it in streams mode
  (with the store bound to the streams view via rebuild).
- **`e` editor handoff** extended to all data modes: events,
  subscriptions, snapshots — each opens its envelope as JSON
  in `$EDITOR` via `tea.ExecProcess` (altscreen suspended).

### Changed

- Cluster is now mode 1, streams mode 2, subs mode 3, snaps
  mode 4. Boot lands on cluster so the first thing you see is
  "is this healthy".
- Ranger layout: 28/32/40 split (was 22/40/38) — col 0 widened
  so 13-20 char stream ids don't get truncated at ~80-cols.
- Stores tab dropped (was mode 5). Topology + health live in
  the always-on header now.

### Notes

- Stream-id display: lazyreckon renders both user
  (`prefix-hex`) and system (`$ns:name`) forms correctly. See
  reckon-db's `guides/system_streams.md` for the convention.

## [0.1.0] - 2026-05-17

### Changed — Ranger three-column layout (breaking)

Tear out the tabs chrome. Layout is now a miller-columns view in the
spirit of `ranger(1)`:

```
header (store + cluster health + endpoint)
─────────────────────────────────────────────
streams  │  events             │  detail
─────────────────────────────────────────────
mode strip (1 streams · 2 subs · 3 snaps)
status (j/k h/l 1-3 e q · clock)
```

- `j`/`k` move within the focused column; `h`/`l` ascend/descend the
  hierarchy; parent selection drives child contents.
- Adaptive collapse: 3 cols ≥ 100w, 2 cols 80-99w (parent
  breadcrumbed), 1 col < 80w (focused only).
- The "stores" tab is gone — topology + health now lives in the
  always-on header, sourced from a top-level `WatchStores` stream.

### Added

- `e` on the streams mode's event opens `$EDITOR` (read-only)
  on `$XDG_CACHE_HOME/lazyreckon/<stream>_v<n>.json` containing
  the full envelope + decoded data + metadata. Falls back through
  `$VISUAL`, `nvim`, `vim`, `nano`, `less`. Bubbletea's altscreen
  is suspended for the duration via `tea.ExecProcess`.
- Subscriptions + snapshots modes ship as 3-column stubs so the
  mode strip works end-to-end; wired in follow-ups as the SDK
  gains the underlying coverage.
- `internal/ranger` — reusable column interface + orchestrator
- `internal/modes` — one wired triple per mode
- `internal/editor` — `$EDITOR` handoff

## [0.1.0] - 2026-05-17

First usable release. Five-tab chrome + the `stores` tab live-streaming
topology from `StoresService.WatchStores`. Visual identity drawn from
the reckon-portal artwork palette (deep cosmic violet, Horus acid
green, Seshat gold, Sienna).

### Added

- `cmd/lazyreckon` — entry point. `--endpoint host:port`, default `localhost:50051`.
- `internal/theme` — lipgloss palette + named styles from reckon-portal artwork.
- `internal/ui` — header, tab strip, status bar (the chrome).
- `internal/panes` — pane interface + first wired pane (`stores`) +
  placeholders for `streams` / `events` / `subscriptions` / `snapshots`.
- Tab navigation: `1`–`5` jump, `tab`/`shift+tab` cycle, vim-style `h`/`l`.
- Per-second clock + age refresh of the topology table.
- Streaming `WatchStores` consumption with snapshot + live updates;
  EventRetired deletes the row, EventAnnounced upserts it.

### Notes

- Bubble Tea altscreen mode — the previous terminal is restored on exit.
- Tabs other than `stores` show a placeholder identifying the
  RPC they'll consume.

Depends on `reckon-go 0.1.x`. Compatible with `reckon-gateway 0.4.x`.
