# reckon-lazy

A terminal UI for the [ReckonDB](https://codeberg.org/reckon-db-org/reckon-db) event store, in the spirit of `lazygit` / `lazydocker` / `k9s` / `ranger`.

The binary is named **`lazyreckon`** to fit the lazy-* family; the repo is `reckon-lazy` for org-naming consistency.

```bash
lazyreckon --endpoint beam01.lab:50051
```

## Layout

Three-column miller-columns view, ranger-style. Header carries the active store and live cluster health; a mode strip at the bottom swaps what the columns list.

```
┌─ ◉ lazyreckon  · store default_store  · ● 4/4 lead .12 ──┐
│                                                          │
│  streams      │  events             │  detail            │
│  foo$1        │  v0 user_v1         │  type:    user_v1  │
│▸ foo$2        │▸ v1 user_v2         │  version: 1        │
│  bar$1        │  v2 order_v1        │  data:             │
│  baz$7        │                     │    { "id": 42 }    │
│                                                          │
├─ 1 streams · 2 subscriptions · 3 snapshots ──────────────┤
│ j/k move  h/l in/out  1-3 mode  e edit  q quit    15:04  │
└──────────────────────────────────────────────────────────┘
```

Adaptive: three columns at ≥100 wide, two at 80-99 (parent breadcrumbed), one at <80 (focused only).

## Modes

| # | Mode | Columns | Status |
|---|---|---|---|
| 1 | cluster | 4-pane grid (top: nodes ↔ node detail; bottom: stores ↔ store info) | ✅ |
| 2 | streams | streams → events → event detail | ✅ |
| 3 | subscriptions | subs → lag → full info | ✅ |
| 4 | snapshots | streams → versions → data | ✅ |

Boot lands on **cluster** so the first thing you see is whether the cluster is healthy (leader, quorum, term, failed nodes). Drill into data with `2`. Subs and snaps land as the [reckon-go](https://codeberg.org/reckon-db-org/reckon-go) SDK gains the underlying wrappers.

## Keys

| Key | Action |
|---|---|
| `j` / `k` (or `↓`/`↑`) | Move within focused column |
| `h` / `l` (or `←`/`→` / `enter`) | Ascend / descend within the focused ranger |
| `tab` | (Cluster mode) Swap focus between top (nodes) and bottom (stores) rangers |
| `g` / `G` | Jump to top / bottom of focused column |
| `1` – `4` | Switch mode (1=cluster, 2=streams, 3=subs, 4=snaps) |
| `enter` (on a store in cluster) | Open the selected store in streams mode |
| `e` | Open selected event in `$EDITOR` (read-only) |
| `q` / `ctrl+c` | Quit |

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
  ui/         header, mode strip, status bar (chrome)
  ranger/     column interface + orchestrator
  modes/      one wired ranger triple per top-level mode
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
