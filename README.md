# reckon-lazy
[![Buy Me A Coffee](https://img.shields.io/badge/Buy%20Me%20A%20Coffee-support-yellow.svg)](https://buymeacoffee.com/rlefever)

A terminal UI for the [ReckonDB](https://codeberg.org/reckon-db-org/reckon-db) event store, in the spirit of `lazygit` / `lazydocker` / `k9s` / `ranger`.

The binary is named **`lazyreckon`** to fit the lazy-* family; the repo is `reckon-lazy` for org-naming consistency.

```bash
lazyreckon --endpoint beam01.lab:50051
```

## Layout

Breadcrumb drill-down, `k9s`-style. The top bar shows the navigation path (`reckon ▸ store ▸ mode ▸ …`); the body shows a slim sibling rail plus a wide child preview, and the leaf (event payload, snapshot data, subscription detail) collapses to a full-width pane so the JSON gets the room it needs. Header carries the active store and live cluster health.

```
┌─ ◉ lazyreckon  · store default_store  · ● 4/4 lead .12 ──┐
│ reckon ▸ default_store ▸ streams ▸ orders-018f6a…        │
│┌ streams ───────────┐┌ orders-018f6a…  events ──────────┐│
││  users-018f6a7b8c…  ││  v0 user_v1            14:02:11  ││
││▸ orders-018f6a7b9d… ││▸ v1 order_placed_v1    14:02:13  ││
││  invoices-018f6a7b… ││  v2 order_confirmed_v1 14:02:15  ││
││  _dcb        [DCB]  ││  v3 lot_released_v1    14:03:01  ││
│└─────────────────────┘└──────────────────────────────────┘│
│ j/k move  h/l drill  1-4 mode  e edit  q quit       15:04 │
└──────────────────────────────────────────────────────────┘
```

`l`/`enter` drills deeper (rail → child → leaf); the leaf zooms to full width. `h` drills back out. The breadcrumb path replaces the old mode strip — the mode is just a segment of the path. Adaptive: rail+preview at every width; the column renderer clamps each line so nothing wraps past the frame.

## Modes

| # | Mode | Drill path | Status |
|---|---|---|---|
| 1 | stores | 4-pane grid (top: stores ↔ store info; bottom: nodes ↔ node detail) | ✅ |
| 2 | streams | streams → events → event detail | ✅ |
| 3 | subscriptions | subs → detail (info + live lag) | ✅ |
| 4 | snapshots | streams → versions → data | ✅ |

Boot lands on **stores** so the first thing you see is whether the cluster is healthy (leader, quorum, term, failed nodes per store) — it keeps its 4-pane dashboard grid rather than a drill path. Drill into data with `2`–`4`.

> **Note**: stores on reckon-db 3.1.1+ expose a `_dcb` pseudo-stream that holds [Dynamic Consistency Boundary](https://codeberg.org/reckon-db-org/reckon-db/src/branch/main/guides/dcb.md) events. It shows up in streams mode with a `DCB` badge so it's visually distinct from aggregate streams. The badge has no behavioural effect — you can browse `_dcb` like any other stream.

## Keys

| Key | Action |
|---|---|
| `j` / `k` (or `↓`/`↑`) | Move within the focused list |
| `l` / `→` / `enter` | Drill in (deeper); the leaf zooms to full width |
| `h` / `←` | Drill back out |
| `tab` | (Cluster mode) Swap focus between top (nodes) and bottom (stores) rangers |
| `g` / `G` | Jump to top / bottom of focused list |
| `1` – `4` | Switch mode (1=stores, 2=streams, 3=subs, 4=snaps) |
| `enter` (on a store in stores mode) | Open the selected store in streams mode |
| `e` | Open selected event / sub / snapshot in `$EDITOR` (read-only) |
| `r` | Refresh the active mode's primary list |
| `?` | Toggle help overlay (mode-aware cheatsheet) |
| `q` / `ctrl+c` | Quit |

`?` is the discoverability hook — press it any time and you get the full mode-specific binding table without leaving the app.

### Editor handoff

`e` on a selected event dumps `{envelope + data + metadata}` as JSON to `$XDG_CACHE_HOME/lazyreckon/<stream>_v<n>.json` and runs `$EDITOR` on it (falls back through `$VISUAL`, `nvim`, `vim`, `nano`, `less`). Bubbletea's altscreen is suspended for the duration; control returns when the editor exits. Writeback is ignored — events are immutable.

## Visual identity

Palette and glyph come from the reckon-portal artwork (Seshat, eye-of-horus, sphere):

- **Deep cosmic violet** base — `#1E0A2E`, `#320B4F`, `#3F125B`, `#4C1D95`
- **Horus acid green** accent — `#B8E234`, `#9BCF20`
- **Seshat gold** for warnings — `#fac53a`
- **Sienna** for failures — `#c13c1b`

Defined in [`internal/theme`](internal/theme/theme.go).

## Layout (source)

```
internal/
  theme/      lipgloss palette + named styles
  ui/         header, breadcrumb, status bar (chrome)
  ranger/     column interface + renderer; Ranger (grid) + Drill (breadcrumb)
  modes/      one wired view per mode (Drill chain, or the cluster grid)
  editor/     $EDITOR handoff via tea.ExecProcess
cmd/lazyreckon/
  main.go     top-level model + key routing
```

## Stack

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI runtime
- [lipgloss](https://github.com/charmbracelet/lipgloss) — styling
- [reckon-go](https://codeberg.org/reckon-db-org/reckon-go) — gRPC client

## Build

```bash
go build -o lazyreckon ./cmd/lazyreckon
```

## License

Apache-2.0.
