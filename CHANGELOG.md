# Changelog

All notable changes to `reckon-lazy` (binary: `lazyreckon`) will be documented in this file.

Format: [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning: [SemVer](https://semver.org/).

## [Unreleased]

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
