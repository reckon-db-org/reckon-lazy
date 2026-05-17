# reckon-lazy

A terminal UI for the [ReckonDB](https://codeberg.org/reckon-db-org/reckon-db) event store, in the spirit of `lazygit` / `lazydocker` / `k9s`.

The binary is named **`lazyreckon`** to fit the lazy-* family; the repo is `reckon-lazy` for org-naming consistency.

```bash
lazyreckon --endpoint beam01.lab:50051
```

## Status

`v0.1.0` — scaffold. Bubble Tea entry point with the planned pane layout. No panes implemented yet — they land as the [reckon-go](https://codeberg.org/reckon-db-org/reckon-go) SDK gains the service wrappers underneath.

## Planned panes

| Pane | Wire source | Status |
|---|---|---|
| Cluster | gateway+raft via `StoresService` | ⬜ |
| Stores | `StoresService.WatchStores` (live) | 🚧 first to land |
| Streams | `AdminService.GetStreamInfo` / `StreamService` | ⬜ |
| Events | `SubscriptionService.Subscribe` (live tail) | ⬜ |
| Subscriptions | `SubscriptionService.ListSubscriptions` + `GetSubscriptionLag` | ⬜ |

Read-only for v0. Write actions (cancel subscription, force snapshot, scavenge) are a v0.x follow-up once the read flow is solid.

## Stack

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the TUI
- [lipgloss](https://github.com/charmbracelet/lipgloss) for styling
- [reckon-go](https://codeberg.org/reckon-db-org/reckon-go) for the gRPC client

## Build

```bash
go build -o lazyreckon ./cmd/lazyreckon
```

## License

Apache-2.0.
