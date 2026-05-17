# reckon-lazy

A terminal UI for the [ReckonDB](https://codeberg.org/reckon-db-org/reckon-db) event store, in the spirit of `lazygit` / `lazydocker` / `k9s`.

The binary is named **`lazyreckon`** to fit the lazy-* family; the repo is `reckon-lazy` for org-naming consistency.

```bash
lazyreckon --endpoint beam01.lab:50051
```

## Status

`v0.1.0` — first usable pane lands. The `stores` tab is live: a streaming view of every (store_id, node) registration in the cluster, refreshed via `StoresService.WatchStores`.

The other four tabs are placeholders; they land as the [reckon-go](https://codeberg.org/reckon-db-org/reckon-go) SDK wrappers gain coverage of the underlying RPCs.

## Tabs

| # | Tab | Wire source | Status |
|---|---|---|---|
| 1 | stores | `StoresService.WatchStores` (live) | ✅ |
| 2 | streams | `StreamService.ListStreams` + `GetStreamVersion` | ⬜ |
| 3 | events | `SubscriptionService.Subscribe` (live tail) | ⬜ |
| 4 | subscriptions | `SubscriptionService.{List, GetSubscriptionLag}` | ⬜ |
| 5 | snapshots | `SnapshotService.ListAllSnapshots` | ⬜ |

Read-only for v0. Write actions (cancel subscription, force snapshot, scavenge) are a follow-up once the read flow is solid.

## Keys

| Key | Action |
|---|---|
| `1` – `5` | Jump to tab |
| `tab` / `→` / `l` | Next tab |
| `shift+tab` / `←` / `h` | Previous tab |
| `q` / `ctrl+c` | Quit |

## Visual identity

Palette and glyph come from the reckon-portal artwork (Seshat, eye-of-horus, sphere):

- **Deep cosmic violet** base — `#1E0A2E`, `#320B4F`, `#3F125B`, `#4C1D95`
- **Horus acid green** accent — `#B8E234`, `#9BCF20`
- **Seshat gold** for warnings — `#fac53a`
- **Sienna** for failures — `#c13c1b`

Defined in [`internal/theme`](internal/theme/theme.go).

## Stack

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the TUI runtime
- [lipgloss](https://github.com/charmbracelet/lipgloss) for styling
- [reckon-go](https://codeberg.org/reckon-db-org/reckon-go) for the gRPC client

## Build

```bash
go build -o lazyreckon ./cmd/lazyreckon
```

## License

Apache-2.0.
