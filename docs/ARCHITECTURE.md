# agent-switch Architecture

## 1. Domain Scope
Controls the AI agent's operational state at runtime through an append-only audit log.
Every state change is persisted as a new row; no data is ever mutated or deleted.

## 2. Core Entity
- **AgentSwitch:** A single audit event: who changed the state, when, why, and to what.
  The latest row (by `changed_at` sort descending) is always the current state.

## 3. Patterns

- **Reusable-module harness**: the module is coupled only to published contracts, not to concrete
  infrastructure — see `AGENTS.md` (this repo's root) for the full whitelist/blacklist this module
  must hold to:
    - `orm.DB` for storage (backend-agnostic — wraps whatever `storage.Conn` the app injects).
    - `ddl.CreateTable` (over `db.RawConn()`, behind a `db.RawConn().(ddl.Compiler)` type
      assertion) for the module's own schema migration in `New()` — replaces the removed
      `orm.DB.CreateTable`. Against `storage/mem` (this module's own tests) it is a no-op; against a
      real SQL backend it migrates the schema.
    - `router.OpModule` (`ModelName()` + `MountOps(reg router.OpRegistry)`) for transport — the
      module never sees `router.Router`/`router.APIModule`, and never imports `tinywasm/mcp`.
    - `model.IDGenerator` for identity (`Deps.IDs`, required — the module never builds its own).
      Unlike the pre-harness version, the module does **not** derive `changed_at` from the ID
      (`model.IDGenerator` exposes only `NewID() string`, no timestamp extraction — that was a
      `unixid`-specific capability). `changed_at` is an explicit `int64` column, set via
      `github.com/tinywasm/time.Now()` at insert time.
    - `events.Publisher` for event-driven updates (`Deps.Publisher`, optional — `nil` disables
      publishing silently). Every successful `Toggle` publishes `TopicAgentToggled` with the new
      `*AgentSwitch` row as payload.
    - **No `view.Presenter`.** Explicit judgment call: a single append-only boolean log with only
      two ops (read current state, append a toggle) has no natural "list → select → save/delete"
      workflow — `view.New` needs a `ListOp` yielding a `model.FielderSlice` to project into
      `[]view.Item`, and neither op serves that role. Revisit if a future op lists toggle history.
    - Tests run against `storage/mem` (`orm.New(mem.New())`), never `tinywasm/sqlite`, from `tests/`
      (package `tests`, external, own `go.mod` with a `replace` back to this module).
- **Append-only log**: INSERT only. No UPDATE, no DELETE. Revert by toggling again. Enforced by
  convention (no `db.Update`/`db.Delete` call exists anywhere in this module) rather than by a DB
  constraint.
- **Read strategy**: `ORDER BY changed_at DESC LIMIT 1` — not `ORDER BY id DESC`. `id` comes from a
  generic `model.IDGenerator` with no time-sortability guarantee (that guarantee was specific to
  `unixid`, the pre-harness generator); `changed_at` is the only reliable ordering key.
- **Typed events**: the published event carries a `model.Encodable` payload (`&AgentSwitch`), never
  a bare `map`.

## 4. Ops (via `MountOps`)

| Op | Action | Resource | Args | Description |
|---|---|---|---|---|
| `get_agent_status` | `r` (`model.Read`) | `agent_switch` | none (`.Accepts(nil)`) | Returns current enabled state, actor, timestamp, and reason — or an empty result if the log has never been toggled |
| `toggle_agent_status` | `u` (`model.Update`) | `agent_switch` | `ToggleArgs{IsEnabled, ChangedBy, Reason}` | Inserts a new audit row (enable or disable); `ChangedBy` is required, `IsEnabled` has no presence check — an omitted key toggles to `false` |

## 5. Composition Root Example

```go
sw, _ := agentswitch.New(db, agentswitch.Deps{
    IDs:       idGenerator,    // model.IDGenerator
    Publisher: eventPublisher, // events.Publisher, nil disables publishing
})
sw.MountOps(opRegistry) // router.OpRegistry — e.g. mcp.HarvestOps(sw, ...) at the transport boundary
// No NewView/view.Presenter for this module (see §3) — a composition root that wants a UI drives
// get_agent_status/toggle_agent_status directly through whatever Caller it already has.
```

## 6. Schema
See [`docs/diagrams/database.md`](diagrams/database.md).
