# Changelog

All notable changes to `reckon-lazy` (binary: `lazyreckon`) will be documented in this file.

Format: [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning: [SemVer](https://semver.org/).

## [Unreleased]

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
