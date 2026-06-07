# DESIGN: Breadcrumb Drill-Down Navigation

**Status:** ✅ Implemented (2026-06-07)
**Date:** 2026-06-07
**Supersedes:** the 3-column `ranger` (miller-columns) layout for the
streams / subscriptions / snapshots modes.

## Implementation notes (deviations from the original draft)

Three things landed differently from §3–§7 below, each to avoid debt:

1. **`Drill` lives in package `ranger`, not a new `internal/drill`.** A
   separate package would have forced duplicating the bordered,
   width-clamped column renderer (the hard-won `MaxWidth` logic). Drill
   reuses it as a free `renderColumn` func alongside `Ranger`. The
   package doc now describes both orchestrators over the shared
   `Column` primitive.
2. **`SyncDetail` is deleted outright, not "kept as a typed bridge".**
   Each leaf detail pane (`eventDetailCol`, `snapDataCol`,
   `subDetailCol`, `nodeDetailCol`) takes a closure at construction that
   reads its parent column's typed selection live at render time. No
   parent→leaf sync for the model to drive — the global
   `model.syncDetail()` switch is gone.
3. **Subscriptions collapsed 3 panes → 2 levels.** `lag` and `info`
   were co-details of one selected subscription, not a hierarchy, so
   they share one detail pane (`subDetailCol`: info via closure + lag
   via the existing `GetSubscriptionLag` RPC on selection change)
   rather than two drill levels.

**Update (2026-06-07): §6 causation drilling shipped.** `ranger.Drill`
gained a dynamic stack via a `Brancher` interface (a column yielding a
child on demand); `l` past the streams detail leaf opens a
`modes.eventNodeCol` (event + cause ▲ / effects ▼), and walking a link
pushes another node, popped + Stop()ed on `h`. The breadcrumb traces
the graph path. Temporal drilling and `Correlated` (which needs a
correlation_id extracted from event metadata, not an event id) remain
follow-ups.

---

## 1. Motivation

The current layout (`internal/ranger/`) renders every drill mode as three
fixed side-by-side columns. That idiom is wrong for lazyreckon's data on two
opposite axes at once:

1. **The leaf is the payload, and the payload is large.** Inspecting an event
   means reading its JSON `data` + `metadata` + `tags` + hash-chain fields.
   That content wants maximum width and height, but miller-columns hand it the
   *least* room — the rightmost preview at ~40% width, which `renderCol`
   already has to `lipgloss.MaxWidth`-clamp to stop it overflowing. The
   `<80` / `<100` adaptive-collapse ladder in `Ranger.View` and the
   `viewinspect` / `btrepro` debug binaries are symptoms: the layout is
   fighting the terminal width budget, and that fight is unwinnable because the
   data genuinely needs the width.

2. **The hierarchy is deep and is a graph, not a tree.** `Store → Stream →
   Event` is already three levels with no room to spare. But reckon-db's
   differentiators — `CausationService` (`GetCause`, `GetEffects`,
   `BuildCausationGraph`), `TemporalService`, the `_dcb` context — mean an
   event has edges to *other events in other streams*. "Follow this event to
   its causation parent, then to that one's effects" is a **path through a
   graph**, which miller-columns physically cannot represent past depth 3. A
   breadcrumb *is* a path, so it represents this natively.

Precedent: **k9s** solves this exact problem class (a TUI browsing a deep,
detail-heavy, navigable backend) with breadcrumb drill-down, deliberately not
miller-columns. The name "lazyreckon" echoes lazygit's *fixed-panel* model, but
lazygit's hierarchy is shallow and fixed (5 known panels); an event store is
deeper and dynamic, so k9s is the better prior than lazygit or ranger.

---

## 2. Chosen design: hybrid drill

Not pure single-pane. Miller's one real strength — seeing siblings while you
inspect one — matters at the *list* levels (scrubbing streams, watching event
rows as you move). So each level picks its own layout:

- **List levels** render master-detail: the current column as a **slim sibling
  rail** (left, ~22 cols) + the selected item's child as a **wide preview**
  (right, ~75%).
- **Leaf** (event detail / snapshot data / subscription info) **zooms to full
  width** for the payload.
- **`stores` mode is unchanged** — cluster topology + health is a dashboard, not
  a path. It keeps its 2×2 grid (`ranger.New2` × 2).

```
reckon ▸ orders ▸ streams ▸ orders-42            [health ●3/3]
┌ streams ──────┐┌ orders-42 events ──────────────────────┐
│ orders-41     ││ #6 payment_authorized_v1   14:02:12     │
│▸orders-42     ││▸#7 lot_occupied_v1         14:02:13     │
│ orders-43     ││ #8 order_confirmed_v1      14:02:15     │
│ _dcb    [DCB] ││ #9 lot_released_v1         14:03:01     │
└───────────────┘└─────────────────────────────────────────┘
 j/k move  l open  h back  / filter  : goto

— press l on #7 → leaf zooms FULL WIDTH —

reckon ▸ orders ▸ orders-42 ▸ #7 lot_occupied_v1     [●3/3]
┌ event #7 ───────────────────────────────────────────────┐
│ tags [lot:5 zone:A]   hash 9f3a…   epoch 1716003733      │
│ ── data ──────────────────────────────────────────────  │
│ { "lot_id": 5, "occupied_by": "ABC-123", "zone": "A" }   │
└──────────────────────────────────────────────────────────┘
```

### Core interaction model

The drill always renders `[ current-as-rail | selected-child-preview-wide ]`.
The child preview is fed the rail's `Selected()` via the **existing**
`Column.SetParentSelection` propagation — this is already what `Ranger.propagate`
does between columns. The differences from `Ranger`:

- Show **2 panes** (current + child preview), not 3. Ancestors live in the
  breadcrumb instead of a third column → the child preview gets ~75% width
  instead of ~33%.
- `l` / Enter **drills**: the child becomes the new current (the old current
  drops into the breadcrumb), a fresh child preview appears.
- When the child preview is a terminal detail (no further child), `l` **zooms**
  it to full width, hiding the rail. `h` **unzooms** back to master-detail.
- `h` / Esc / Backspace pops a level (and `Stop()`s the popped column).

This means `l` reads naturally as "go deeper / zoom in" and `h` as "back / zoom
out" at every level.

---

## 3. New components

### `internal/drill/drill.go` — the orchestrator

Reuses `ranger.Column` verbatim (no interface change).

```go
package drill

type level struct {
    col   ranger.Column
    leaf  bool // terminal detail: zoomable, no child
}

type Drill struct {
    stack []level   // stack[0] = root; top = current
    zoom  bool       // leaf rendered full-width
}

func New(cols ...ranger.Column) *Drill   // cols[last] is the leaf
func (d *Drill) Init() tea.Cmd
func (d *Drill) Update(tea.Msg) tea.Cmd
func (d *Drill) HandleKey(key string) (tea.Cmd, bool) // j/k/l/h/g/G
func (d *Drill) SetFilter(needle string) tea.Cmd       // delegates to current
func (d *Drill) GotoID(needle string) (tea.Cmd, bool)
func (d *Drill) Crumbs() []string  // per-level label for the breadcrumb
func (d *Drill) View(width, height int) string
func (d *Drill) Stop()             // Stop()s every column in the stack
```

`View` rule:
- `zoom` → render top column full-width.
- depth ≥ 2 → master-detail: `stack[top-1]` as rail (the level you're standing
  in) + `stack[top]` as wide preview. (Or current + synthetic child; see §5.)
- depth 1 → single full-width list (root, no parent yet).

`HandleKey`:
- `j`/`k` → `current.Move(±1)` then re-`propagate`.
- `l`/Enter → if next level exists, push it (built from current `Selected()`)
  and propagate; if current is the leaf, toggle `zoom = true`.
- `h`/Esc → if `zoom`, `zoom = false`; else pop + `Stop()` the popped column.
- `g`/`G` → top/bottom of current.

Propagation reuses the `ranger.propagate` logic (factor it into a shared helper
or copy — it is ~10 lines).

### `internal/ui/breadcrumb.go` — the path bar

```go
func Breadcrumb(segments []string, width int) string
```

- Joins segments with ` ▸ `, styled (root + leaf emphasized).
- **Width discipline:** if the joined path exceeds `width`, elide middle
  segments to `root ▸ … ▸ parent ▸ leaf` (keep first + last two). This is the
  one new place that can overflow; clamp it the same way `renderCol` clamps.
- Ancestor jump: number keys `1..9` (or `:N`) pop the stack to depth N.

---

## 4. Per-mode migration (mode-builder pattern survives)

The `Build*` functions barely change — same Columns, swap `ranger.New` for
`drill.New`, delete the `SyncDetail` side-channel (detail becomes a normal drill
level fed by `SetParentSelection`).

```go
// internal/modes/streams.go — before
Ranger: ranger.New(streamsCol, eventsCol, detailCol),
// after
Drill:  drill.New(streamsCol, eventsCol, detailCol), // detailCol is the leaf
```

Same one-line swap for `subscriptions.go` (`listCol, lagCol, infoCol`) and
`snapshots.go` (`streamsCol, versionsCol, dataCol`). `stores.go` is untouched.

`SyncDetail()` and its three call sites are **removed** — the leaf column now
receives the selection through `SetParentSelection`, exactly like every other
child. (Note: `stores_render_test.go:46` and the `SyncDetail` methods go with
it; update that test to drive the stores dashboard directly.)

---

## 5. Mode folds into the breadcrumb

Today there are two navigation models: the number-key **mode strip**
(`modeIdx` in `main.go`) and the in-mode **h/l columns**. Unify them: the mode
becomes the second breadcrumb segment.

```
reckon ▸ <activeStore> ▸ <mode> ▸ <drill path…>
         └ store segment        └ owned by the active Drill
```

- `main.go` owns the leading `reckon ▸ store ▸ mode` segments; the active
  `Drill.Crumbs()` supplies the rest.
- Number keys `1..4` still switch modes (cheap), but the **mode strip widget is
  replaced by the breadcrumb bar** in the header. `m.activeRanger()` becomes
  `m.activeDrill()`; `bind*ToActive` rebuild a `*drill.Drill` instead of a
  `*ranger.Ranger` (the lazy-binding logic is otherwise identical).
- `jumpToStreams(store)` still works: it rebinds the streams drill and switches
  mode; the breadcrumb just reflects the new path.

---

## 6. Causation unlock (the payoff)

At the event-detail leaf, add child factories that push onto the **same stack**,
so the breadcrumb shows graph traversal:

```
reckon ▸ orders ▸ orders-42 ▸ #7 lot_occupied ▸ →cause ▸ shipment-9 ▸ #2
```

New leaf keybindings (each pushes a new level rooted at the linked event):
- `c` → `Causation(store).Cause(eventID)` → drill to the causing event.
- `e` → `Causation(store).Effects(eventID)` → drill to a list of caused events.
- `G` (or a dedicated key) → `Causation(store).Graph(eventID)` → a tree column.

These are pure additions — impossible to express in fixed 3-column miller, and
they align with reckon-db's actual differentiators (causation graphs, hash
chains, DCB context). Ship them after the core swap lands.

---

## 7. Implementation steps

1. `internal/drill/drill.go` — orchestrator (stack, 2-pane + zoom layout,
   reuse `ranger.Column`, reuse propagate).
2. `internal/ui/breadcrumb.go` — path bar + middle-elision + ancestor jump.
3. Swap `ranger.New` → `drill.New` in `streams.go`, `subscriptions.go`,
   `snapshots.go`; delete `SyncDetail` and its call sites.
4. `main.go`: `activeRanger`→`activeDrill`, `bind*ToActive` build Drills,
   replace the mode strip with the breadcrumb header. Keep `stores` on ranger.
5. Verify render-height budget with `btrepro`; retire `viewinspect` (its job —
   diagnosing column-width alignment — disappears with the 3-col layout).
6. Add causation drills at the event leaf (§6).
7. Update `README.md` (controls) and `CHANGELOG.md`.

`ranger` is **not deleted** — `stores` mode keeps using `ranger.New2`. It just
stops being the navigation model for the drill modes.

---

## 8. Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| Breadcrumb overflow on narrow terminals | Middle-segment elision (`root ▸ … ▸ leaf`); clamp like `renderCol`. |
| Loss of sibling context at list levels | Hybrid keeps the slim rail; only the leaf is full-width. |
| "Zoom" affordance unclear | Status-bar hint (`l zoom · h back`) + a `⤢` glyph on the zoomable leaf border. |
| Leaked background work (`WatchStores`, tickers) on pop | `Drill.Stop()` / pop must call the popped `Column.Stop()`. |
| Deep causation drills grow the stack unbounded | Cap visible breadcrumb segments; `Stop()` on pop frees columns. |

---

## 9. What carries over unchanged

- The entire `ranger.Column` interface and every existing Column implementation.
- `SetParentSelection` propagation (the master→detail wiring).
- `/` filter and `:` goto (per-Column, delegated by the orchestrator).
- The lazy mode-binding pattern (`bind*ToActive`) and `activeStore` decoupling.
- `stores` mode (2×2 dashboard) and its health probing.
