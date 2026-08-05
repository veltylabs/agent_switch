---
PLAN: "feat: agent_switch joins the reusable-module harness (OpModule, IDGenerator, ddl, storage/mem tests)"
TAG: v0.1.0
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 11670371054963981710
---

> This plan is dispatched via the CodeJob workflow. See skill: **agents-workflow**.

# PLAN — agent_switch joins the reusable-module harness

You are an agent with **zero prior context** and **only this repository** (`agent_switch`). This
plan is fully self-contained: every contract, rule, and code sample you need is inline below. Read
`AGENTS.md` (this repo's root, copied verbatim from `veltylabs/modules/AGENTS.md`) before touching
anything — it states the whitelist/blacklist of imports this plan applies and the exact shape
(`Deps`/`Module`/`New`/`ddl.CreateTable`/`router.OpModule`) every rectified module in this org takes.
The reference implementation this plan replicates is `github.com/veltylabs/item_catalog` — its
current code (not its own `docs/PLAN.md`, which is a narrower, unrelated bugfix) is the pattern
source cited throughout.

## 0. What this module is, today

`agent_switch` is an append-only audit log for one boolean: whether an AI agent is enabled. Every
state change is a new INSERT; nothing is ever UPDATEd or DELETEd; "current status" is whichever row
is latest. It currently:

- Implements `mcp.ToolProvider` (`Tools() []mcp.Tool`) directly — a concrete transport.
- Constructs `unixid.NewUnixID()` itself inside `New()` — a concrete ID generator.
- Calls the removed `db.CreateTable(&AgentSwitch{})` (an `*orm.DB` method that no longer exists in
  current `orm`).
- Encodes/decodes JSON by hand via `github.com/tinywasm/json` and reads MCP request bodies as raw
  strings (`strings.Contains(req.Params.Arguments, "is_enabled")`).
- Tests via `github.com/tinywasm/sqlite` (`:memory:`), in-package (`package agentswitch`), not in
  `tests/`.
- `changed_at` (the audit timestamp) does not exist as a column: `GetStatus` parses it out of the
  `id` string via `m.uid.Parse(r.ID)` — a capability specific to `unixid`, relying on its
  time-sortable ID encoding.

This plan removes all five and lands the module on the same five-contract shape
(`model`+`router`+`view-or-not`+`events`+`orm`/`ddl`) as `item_catalog`. `go.mod` currently pins
badly stale versions (`orm v0.6.0`, `mcp v0.1.1`, `unixid v0.2.23`, `sqlite v0.2.0`) — Stage 7 below
retargets every dependency to what `item_catalog@main` (the reference, post-cleanup) uses — re-check
that repo's `go.mod` at execution time; as of this revision: `model
v0.1.2`, `orm v0.11.4`, `router v0.1.19`, `ddl v0.0.7`, `events v0.0.2`, `form v0.3.13`, `fmt
v0.25.5`, `time v0.5.2`.

## 1. Resolved design decisions — read before writing any code

Two decisions were open in the source task for this plan and are now resolved. **Both are flagged
for a human/reviewing-agent double-check** — they are the least mechanical part of this migration.

### 1a. `model.IDGenerator` cannot parse a timestamp back out of an ID

`model.IDGenerator` (`github.com/tinywasm/model@v0.1.2`, `interface.go`, unchanged since `v0.0.16`) is:

```go
type IDGenerator interface {
    NewID() string
}
```

No `Parse` method — that was always a `unixid`-specific capability (`unixid.UnixID.Parse(id) (int64,
string, error)`), never part of the port. The current `GetStatus` depends on it:

```go
// mcp.go — CURRENT, to delete
ts, _, err := m.uid.Parse(r.ID)
if err != nil {
    return &mcp.Result{IsError: true, Content: err.Error()}, nil
}
```

**Resolution:** add an explicit `changed_at int64` column to `AgentSwitch`, set once at insert time
via `github.com/tinywasm/time.Now()` — the exact pattern `item_catalog` already uses for
`CatalogItem.UpdatedAt` (`item.UpdatedAt = time.Now()`, see its `mcp.go`). This is a genuine, additive
schema change (one new NOT NULL column), not a like-for-like port — call it out as such in the PR
description. A consequence that must also change: the old read strategy `ORDER BY id DESC LIMIT 1`
("latest row by id descending is current state", per the current `docs/ARCHITECTURE.md`) relied on
`unixid`'s specific guarantee that IDs are time-sortable. A generic `model.IDGenerator` makes no such
guarantee, so the new read strategy is `ORDER BY changed_at DESC LIMIT 1` — see Stage 5. No local
"timestamp-parsing" interface is declared to route around this (that would be exactly the kind of
self-declared port `AGENTS.md`'s blacklist rejects); the explicit column is the correct fix, not a
workaround.

There is no production data to migrate — per the org's master plan
(`https://github.com/tinywasm/app/blob/main/docs/REUSABLE_MODULES_MASTER_PLAN.md`, Fase E), this
module's code has not shipped yet, so there is no backward-compatibility burden from adding a column.

### 1b. Dropping the "`is_enabled` key must be present" check

The current `Toggle` handler does:

```go
// mcp.go — CURRENT, to delete
if !strings.Contains(req.Params.Arguments, "is_enabled") {
    return &mcp.Result{IsError: true, Content: fmt.Err("params", "invalid").Error()}, nil
}
```

— a raw substring search over the still-encoded request body, checking that the caller *sent* the
key at all (both `true` and `false` are valid values once sent; the current `toggleArgs` doc comment
says so explicitly). This is only possible because the current code holds the request as an
undecoded JSON string. Once decoding goes through `router.Context.Decode` into a typed
`ToggleArgs` (`model.Decodable`, walked field-by-field by whatever codec the transport installs — see
`AGENTS.md`, "Encoding"), there is no wire-level way to distinguish "key absent" from "key present
with value `false`" for a plain `model.Bool()`/`input.Checkbox()` field — the same limitation every
other bool field in this ecosystem already lives with (e.g. `item_catalog.CatalogItem.IsActive`).
Re-implementing the old check would mean parsing the raw body as a map before `Decode` — reintroducing
exactly the concrete-encoder coupling this migration removes.

**Resolution:** drop the presence check. A `toggle_agent_status` call that omits `is_enabled`
succeeds and toggles to `false` (the zero value) — this is a genuine behavior change from today,
where the same call was rejected as invalid params. `changed_by` stays a hard requirement (empty
string is unambiguous, no absent-vs-zero-value ambiguity) — see Stage 3.

## 2. Stage 1 — `model.go`: struct+tags → `model.Definition`

Current `model.go` (full file):

```go
//go:build !wasm

package agentswitch

// AgentSwitch records a single agent enable/disable event.
// Append-only: INSERT only. No UPDATE. No DELETE.
type AgentSwitch struct {
	ID        string `db:"pk"` // set by caller via unixid before db.Create()
	IsEnabled bool   `db:"not_null"`
	ChangedBy string `db:"not_null"` // actor identity injected by the application layer
	Reason    string // optional free-text reason
}

// ormc:formonly
type statusEmptyResult struct {
	IsEnabled bool
	ChangedAt int64
}

// ormc:formonly
type statusResult struct {
	IsEnabled bool
	ChangedBy string
	ChangedAt int64
	Reason    string
}

// ormc:formonly
// toggleArgs holds the incoming JSON parameters for the Toggle handler.
// IsEnabled uses plain bool; both true and false are valid toggle values.
// ChangedBy is required (db:"not_null").
type toggleArgs struct {
	IsEnabled bool
	ChangedBy string `db:"not_null"`
	Reason    string
}

// ormc:formonly
type toggleResult struct {
	OK        bool
	IsEnabled bool
}
```

There are exactly 5 `model.Definition`s to write. This breakdown was verified against the
`model_orm.go` this repo has generated **today** (old API, with a separate `Widget:` field per
schema entry): `AgentSwitchModel` has no widget on any field. The widget policy is by **role**, not
by "what the old generated file had": widget kinds (`input.X()`) go ONLY on user-editable fields —
here, the fields of `ToggleArgsModel` (the one record a user fills in). Machine-managed fields and
**output-only result models** (`StatusEmptyResultModel`, `StatusResultModel`, `ToggleResultModel`)
use base kinds (`model.X()`): a result is never rendered as an editable form, and a widget on it
would make `form.New` produce editable inputs for data the user must not touch. (The old
`model_orm.go` had widgets on every transport field — that was a defect this migration corrects,
not a split to preserve.) Dropping a widget from a genuinely form-bound field still silently renders
an empty form (see `AGENTS.md`) — which is why `ToggleArgsModel` keeps its widgets.
The full `model.Kind`/`model.Definition` contract (constructors, `FieldDB`, why `Kind`
is an interface not an enum literal) is documented once in `AGENTS.md` ("The whitelist" +
"Data Model" in `veltylabs/modules/README.md`) — it is not re-pasted here.

Target `model.go` (full file — note the **new** `changed_at` field on `AgentSwitchModel`, absent from
the struct above, and the **new** import of `github.com/tinywasm/fmt` for the one domain error this
file also carries per the ecosystem convention — see `veltylabs/modules/README.md` §"Data Model" for
why domain errors live in `model.go`). Note the old file's `//go:build !wasm` tag is **dropped**:
every import below (`fmt`/`form/input`/`model`) is isomorphic, and keeping the tag would break the
`GOOS=js GOARCH=wasm` build the acceptance criteria (§11) require — with every file tagged `!wasm`
the wasm build has no Go files at all:

```go
package agentswitch

import (
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/form/input"
	"github.com/tinywasm/model"
)

var ErrChangedByRequired = fmt.Err("changed_by is required")

// AgentSwitchModel: append-only audit row. No UPDATE, no DELETE — see AGENTS.md
// "Domain-specific notes". No widget on any field: nothing renders this record
// directly (same as today's generated model_orm.go, which has no Widget: on any
// of AgentSwitch's fields — do not add one here).
//
// changed_at is a NEW column (not present in the struct+tags version this replaces).
// It replaces deriving the timestamp from the id (unixid-specific; model.IDGenerator
// exposes only NewID() string) — see docs/PLAN.md §1a for the full rationale. Set at
// insert time via github.com/tinywasm/time.Now() (Stage 3); read via
// ORDER BY changed_at DESC LIMIT 1 (Stage 5), not ORDER BY id DESC.
var AgentSwitchModel = model.Definition{
	Name: "agent_switch",
	Fields: model.Fields{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "is_enabled", Type: model.Bool(), NotNull: true},
		{Name: "changed_by", Type: model.Text(), NotNull: true},
		{Name: "changed_at", Type: model.Int(), NotNull: true},
		{Name: "reason", Type: model.Text()},
	},
}

// The 4 Definitions below are transport-only (DB: nil on every field, implicitly —
// never set DB at all) — args/results of this module's two ops. Widget policy (see
// the note above §2's target file): ToggleArgsModel is the only user-editable record,
// so it alone carries input widgets; the three result models are output-only and use
// base kinds — a result must never render as an editable form.

var StatusEmptyResultModel = model.Definition{
	Name: "status_empty_result",
	Fields: model.Fields{
		{Name: "is_enabled", Type: model.Bool()},
		{Name: "changed_at", Type: model.Int()},
	},
}

var StatusResultModel = model.Definition{
	Name: "status_result",
	Fields: model.Fields{
		{Name: "is_enabled", Type: model.Bool()},
		{Name: "changed_by", Type: model.Text()},
		{Name: "changed_at", Type: model.Int()},
		{Name: "reason", Type: model.Text()},
	},
}

var ToggleArgsModel = model.Definition{
	Name: "toggle_args",
	Fields: model.Fields{
		{Name: "is_enabled", Type: input.Checkbox()},
		{Name: "changed_by", Type: input.Text(), NotNull: true},
		{Name: "reason", Type: input.Text()},
	},
}

var ToggleResultModel = model.Definition{
	Name: "toggle_result",
	Fields: model.Fields{
		{Name: "ok", Type: model.Bool()},
		{Name: "is_enabled", Type: model.Bool()},
	},
}
```

After writing this file, regenerate `model_orm.go` with `ormc` (run from the module root:
`go install github.com/tinywasm/ormc/cmd/ormc@latest && ormc`). **Do not hand-edit
`model_orm.go`** — it is fully generated. The generated `AgentSwitch` struct will have `Id string`
(pure casing — `id` → `Id`, not `ID`; every reference to `.ID` elsewhere in the repo must become
`.Id`) plus `IsEnabled bool`, `ChangedBy string`, `ChangedAt int64`, `Reason string`, and an
`AgentSwitch_` meta struct (`AgentSwitch_.Id`, `AgentSwitch_.ChangedAt`, …) used for query building in
Stage 5.

## 3. Stage 2 — `mcp.go`: `Deps`, `New`, identity, events

Current `mcp.go` `Module`/`New` (relevant excerpt):

```go
type Module struct {
	db  *orm.DB
	uid *unixid.UnixID
}

func New(db *orm.DB) (*Module, error) {
	if err := db.CreateTable(&AgentSwitch{}); err != nil {
		return nil, err
	}
	u, err := unixid.NewUnixID()
	if err != nil {
		return nil, err
	}
	return &Module{db: db, uid: u}, nil
}
```

Target — `Deps` carries `model.IDGenerator` (required) and `events.Publisher` (optional, matches
`item_catalog.Deps` exactly):

```go
type Deps struct {
	IDs       model.IDGenerator // required — the module never builds its own
	Publisher events.Publisher  // optional — nil disables publishing silently
}

type Module struct {
	db  *orm.DB
	ids model.IDGenerator
	pub events.Publisher
}

func New(db *orm.DB, deps Deps) (*Module, error) {
	if deps.IDs == nil {
		return nil, fmt.Err("agent_switch: Deps.IDs is required")
	}
	if ddlCompiler, ok := db.RawConn().(ddl.Compiler); ok {
		if err := ddl.New(db.RawConn(), ddlCompiler).CreateTable(&AgentSwitch{}); err != nil {
			return nil, err
		}
	}
	return &Module{db: db, ids: deps.IDs, pub: deps.Publisher}, nil
}
```

(The `ddl.CreateTable` type-assertion is Stage 5's persistence change; it is shown here too because
it lives in the same `New()` function — write it once.)

Service methods — replace `GetStatus`/`Toggle`'s current `mcp.Request`/`*context.Context` signatures
(deleted in Stage 4) with plain Go methods the op handlers call:

```go
// GetStatus returns the latest audit row, or nil if the log is empty (never toggled yet).
func (m *Module) GetStatus() (*AgentSwitch, error) {
	rows, err := ReadAllAgentSwitch(
		m.db.Query(&AgentSwitch{}).OrderBy(AgentSwitch_.ChangedAt).Desc().Limit(1),
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// Toggle inserts a new audit row. Append-only — never updates or deletes existing rows.
func (m *Module) Toggle(args ToggleArgs) (*AgentSwitch, error) {
	if args.ChangedBy == "" {
		return nil, ErrChangedByRequired
	}
	row := &AgentSwitch{
		Id:        m.ids.NewID(),
		IsEnabled: args.IsEnabled,
		ChangedBy: args.ChangedBy,
		ChangedAt: time.Now(),
		Reason:    args.Reason,
	}
	if err := m.db.Create(row); err != nil {
		return nil, err
	}
	if m.pub != nil {
		m.pub.Publish(events.Event{Topic: TopicAgentToggled, Payload: row})
	}
	return row, nil
}
```

`events.Publisher.Publish` takes no error return (`Publish(e Event)` —
`github.com/tinywasm/events@v0.0.2`, `events.go`); do not write `if err := m.pub.Publish(...); err !=
nil`, it will not compile. Add the topic constant next to the `Op*` constants in Stage 4 (this module
follows `item_catalog`'s actual layout: `Op*`/`Topic*`/domain errors live in `mcp.go`, next to
`Module`/`Deps`/`New` — not in `model.go`, which holds only `Definition`s and the one error tied
directly to a `Definition`'s field, `ErrChangedByRequired`).

## 4. Stage 3 — imports and file-level cleanup in `mcp.go`

Current top-of-file imports (to delete in full):

```go
import (
	"strings"

	"github.com/tinywasm/context"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/json"
	"github.com/tinywasm/mcp"
	"github.com/tinywasm/orm"
	"github.com/tinywasm/unixid"
)
```

Target imports:

```go
import (
	"github.com/tinywasm/ddl"
	"github.com/tinywasm/events"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/model"
	"github.com/tinywasm/orm"
	"github.com/tinywasm/router"
	"github.com/tinywasm/time"
)
```

`strings`, `context`, `json`, `mcp`, `unixid` all go — none survive anywhere in this file.

## 5. Stage 4 — transport: `router.OpModule` replaces `mcp.ToolProvider`

Current (full):

```go
// Tools implements mcp.ToolProvider.
func (m *Module) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "get_agent_status",
			Description: "Returns the current agent enabled/disabled status.",
			Resource:    "agent_switch",
			Action:      'r',
			Execute:     m.GetStatus,
		},
		{
			Name:        "toggle_agent_status",
			Description: "Enables or disables the agent. Append-only audit log.",
			Resource:    "agent_switch",
			Action:      'u',
			Execute:     m.Toggle,
		},
	}
}

func (m *Module) GetStatus(ctx *context.Context, req mcp.Request) (*mcp.Result, error) { /* ... */ }
func (m *Module) Toggle(ctx *context.Context, req mcp.Request) (*mcp.Result, error) { /* ... */ }
```

Target: delete `Tools()` entirely. `GetStatus`/`Toggle` keep those exact names but change signature
to the plain Go methods already shown in Stage 2 (§3) — they no longer take `mcp.Request`/return
`*mcp.Result`. Add `ModelName`, `MountOps`, and one `opXxx` handler per tool, preserving both tool
names as `Op` string constants (so a transport harvesting these ops — e.g. `mcp.HarvestOps`, see
`AGENTS.md` "Transport" — advertises the same names as today) and preserving `Resource: "agent_switch"`.
`get_agent_status` maps to `model.Read`; `toggle_agent_status` maps to `model.Update` — it is an
append (INSERT), but semantically it *changes* the switch's state, which is what an external caller's
RBAC grant should gate on:

```go
const (
	OpGetAgentStatus    = "get_agent_status"
	OpToggleAgentStatus = "toggle_agent_status"

	TopicAgentToggled = "agent_switch.toggled"
)

func (m *Module) ModelName() string { return "agent_switch" }

func (m *Module) MountOps(reg router.OpRegistry) {
	reg.Op(OpGetAgentStatus, m.opGetAgentStatus).Requires("agent_switch", model.Read).Accepts(nil)
	reg.Op(OpToggleAgentStatus, m.opToggleAgentStatus).Requires("agent_switch", model.Update).Accepts(&ToggleArgs{})
}

var _ router.OpModule = (*Module)(nil)

func (m *Module) opGetAgentStatus(ctx router.Context) {
	row, err := m.GetStatus()
	if err != nil {
		ctx.WriteStatus(500)
		return
	}
	if row == nil {
		if err := ctx.Encode(&StatusEmptyResult{}); err != nil {
			ctx.WriteStatus(500)
		}
		return
	}
	out := &StatusResult{
		IsEnabled: row.IsEnabled,
		ChangedBy: row.ChangedBy,
		ChangedAt: row.ChangedAt,
		Reason:    row.Reason,
	}
	if err := ctx.Encode(out); err != nil {
		ctx.WriteStatus(500)
	}
}

func (m *Module) opToggleAgentStatus(ctx router.Context) {
	var args ToggleArgs
	if err := ctx.Decode(&args); err != nil {
		ctx.WriteStatus(400)
		return
	}
	// Fail-closed doctrine: decode → validate → service. Validate runs the Definition's
	// declared constraints (NotNull on changed_by) — the generated method, never re-implemented.
	if err := args.Validate(model.ActionCreate); err != nil {
		ctx.WriteStatus(400)
		return
	}
	row, err := m.Toggle(args)
	if err != nil {
		if err == ErrChangedByRequired {
			ctx.WriteStatus(400)
		} else {
			ctx.WriteStatus(500)
		}
		return
	}
	if err := ctx.Encode(&ToggleResult{Ok: true, IsEnabled: row.IsEnabled}); err != nil {
		ctx.WriteStatus(500)
	}
}
```

**Casing note, verified by actually generating `model_orm.go` from `ToggleResultModel` and building
against it:** the field is `Ok`, not `OK` — pure-casing applies to every generated field, and "OK"
is not a recognized acronym expansion the way "ID"/"SKU" are (same rule already flagged for `id`→`Id`
in Stage 1, just as easy to miss here since `Ok` "looks wrong" next to the literal `true`).

`get_agent_status` takes no arguments — `.Accepts(nil)` is correct and intentional (`router.Route`'s
doc comment: "`nil` means 'no args'", `route.go` in `tinywasm/router@v0.1.19`, still current), do not
invent an empty args struct for it.

**Do not rename `mcp.go`.** It no longer imports `tinywasm/mcp` after this stage, exactly like
`item_catalog`'s own `mcp.go` — its filename is history, not a whitelist violation; renaming it is
pure churn outside this plan's scope (see `item_catalog/docs/PLAN.md` §5, same call made there).

## 6. Stage 5 — view: explicit decision, no `view.go` in this module

**Judgment: do not add `NewView`/`view.go`.** A single append-only boolean log with only two ops
(read current state, append a toggle) has no natural "list → select → save/delete" workflow —
`view.New` needs a `ListOp` returning a `model.FielderSlice` to project into `[]view.Item`, and
neither existing op serves that role (`get_agent_status` returns one derived record, not a list;
there is no "list all history" op to build one from, and inventing one is out of this plan's scope).
Forcing this into `view.New`'s shape would mean adding a listing op with no current caller, purely to
satisfy the presenter contract. If a later stage adds a "list toggle history" op, revisit this
decision — it is explicit, not a placeholder omission. **This is one of the two calls flagged for a
double-check** (the other is §1a).

## 7. Stage 6 — persistence: `ddl.CreateTable`

Already written in full in Stage 2 (§3) — `New()`'s body. No separate change needed here; this stage
exists only so the acceptance criteria (§11) can point at one place. Confirm after Stage 2 that:
`db.CreateTable(&AgentSwitch{})` (the removed `*orm.DB` method) does not appear anywhere, and the
`ddl.New(db.RawConn(), ddlCompiler).CreateTable(&AgentSwitch{})` call is guarded by the `ok` from the
type assertion on `db.RawConn().(ddl.Compiler)` — never called unconditionally.

## 8. Stage 7 — tests: move to `tests/`, drop `sqlite`, use `storage/mem`

Current `mcp_test.go` (root, `package agentswitch`, `!wasm`) uses `tinywasm/sqlite` and
`tinywasm/context`/`tinywasm/mcp`; every one of its 9 test functions is ported or explicitly retired:

| Current test | Fate |
|---|---|
| `TestTools` | Replaced by `TestMountOps_RegistersBothOps` (router/mock, asserts both ops routed + RBAC denies without a grant) |
| `TestGetAgentStatus_Enabled` | Ported as `TestGetStatus_Enabled` |
| `TestGetAgentStatus_NoHistory` | Ported as `TestGetStatus_NoHistory` |
| `TestGetAgentStatus_ReturnsLatestOnly` | Ported as `TestGetStatus_ReturnsLatestOnly` (now via 3x `Toggle`, ordered by `changed_at`, not by manually constructed `id`) |
| `TestToggleAgentStatus_Enable` | Ported as `TestToggle_Enable` |
| `TestToggleAgentStatus_MissingIsEnabled` | **Deleted** — see §1b; the behavior it asserted (missing `is_enabled` ⇒ error) no longer holds, and there is no post-hoc replacement assertion, because the new behavior ("absent ⇒ toggles to `false`") is already covered by the zero-value path inside `TestToggle_Enable`'s sibling cases if you choose to add one — optional, not required by this plan |
| `TestToggleAgentStatus_MissingChangedBy` | Ported as `TestToggle_MissingChangedBy` (asserts `ErrChangedByRequired`) |
| `TestToggleAgentStatus_AppendOnly` | Ported as `TestToggle_AppendOnly` |

**Do NOT create a `tests/go.mod`.** `tests/` is a plain `package tests` directory **inside the root
module** — the convention `item_catalog` landed on after review (its earlier nested
`tests/go.mod` + `replace` was removed as a defect: `AGENTS.md` says a local-path `replace` is
always a defect to close). Test-only dependencies (`storage/mem`, `router/mock`) go in the root
`go.mod`; `go mod tidy` at the module root resolves everything.

New file `tests/setup_test.go` (`package tests`):

```go
package tests

import (
	"github.com/tinywasm/events"
	"github.com/tinywasm/fmt"
)

type MockPublisher struct {
	Events []events.Event
}

func (m *MockPublisher) Publish(e events.Event) {
	m.Events = append(m.Events, e)
}

var _ events.Publisher = (*MockPublisher)(nil)

type MockIDGen struct {
	counter int
}

func (m *MockIDGen) NewID() string {
	m.counter++
	return "test-id-" + fmt.Convert(m.counter).String()
}
```

New file `tests/agent_switch_test.go` (`package tests`) — representative ports (write the remaining
ones listed in the table above the same way: construct via `setup`, call the exported service method
or drive it through `MountOps`+`router/mock`, assert on the returned/encoded values):

```go
package tests

import (
	"testing"

	agentswitch "github.com/veltylabs/agent_switch"
	"github.com/tinywasm/model"
	"github.com/tinywasm/orm"
	"github.com/tinywasm/router/mock"
	"github.com/tinywasm/storage/mem"
)

func setup(t *testing.T) (*agentswitch.Module, *MockPublisher) {
	t.Helper()
	db := orm.New(mem.New())
	pub := &MockPublisher{}
	m, err := agentswitch.New(db, agentswitch.Deps{IDs: &MockIDGen{}, Publisher: pub})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, pub
}

func TestGetStatus_NoHistory(t *testing.T) {
	m, _ := setup(t)
	row, err := m.GetStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row != nil {
		t.Fatalf("expected nil row for empty log, got %+v", row)
	}
}

func TestGetStatus_Enabled(t *testing.T) {
	m, _ := setup(t)
	if _, err := m.Toggle(agentswitch.ToggleArgs{IsEnabled: true, ChangedBy: "u1", Reason: "test"}); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	row, err := m.GetStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row == nil || !row.IsEnabled || row.ChangedBy != "u1" {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestGetStatus_ReturnsLatestOnly(t *testing.T) {
	m, _ := setup(t)
	for _, a := range []agentswitch.ToggleArgs{
		{IsEnabled: true, ChangedBy: "u1", Reason: "1"},
		{IsEnabled: true, ChangedBy: "u2", Reason: "2"},
		{IsEnabled: false, ChangedBy: "u3", Reason: "3"},
	} {
		if _, err := m.Toggle(a); err != nil {
			t.Fatalf("Toggle: %v", err)
		}
	}
	row, err := m.GetStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row == nil || row.IsEnabled || row.ChangedBy != "u3" {
		t.Fatalf("expected latest row (u3, disabled), got %+v", row)
	}
}

func TestToggle_Enable(t *testing.T) {
	m, pub := setup(t)
	row, err := m.Toggle(agentswitch.ToggleArgs{IsEnabled: true, ChangedBy: "u1", Reason: "test"})
	if err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if !row.IsEnabled || row.ChangedBy != "u1" || row.Id == "" || row.ChangedAt == 0 {
		t.Fatalf("unexpected row: %+v", row)
	}
	if len(pub.Events) != 1 || pub.Events[0].Topic != agentswitch.TopicAgentToggled {
		t.Fatalf("expected 1 TopicAgentToggled event, got %+v", pub.Events)
	}
}

func TestToggle_MissingChangedBy(t *testing.T) {
	m, _ := setup(t)
	_, err := m.Toggle(agentswitch.ToggleArgs{IsEnabled: true})
	if err != agentswitch.ErrChangedByRequired {
		t.Fatalf("expected ErrChangedByRequired, got %v", err)
	}
}

func TestToggle_AppendOnly(t *testing.T) {
	m, _ := setup(t)
	if _, err := m.Toggle(agentswitch.ToggleArgs{IsEnabled: true, ChangedBy: "u1"}); err != nil {
		t.Fatalf("Toggle 1: %v", err)
	}
	if _, err := m.Toggle(agentswitch.ToggleArgs{IsEnabled: false, ChangedBy: "u2"}); err != nil {
		t.Fatalf("Toggle 2: %v", err)
	}
	rows, err := m.History()
	if err != nil {
		t.Fatalf("unexpected error querying db: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows in DB (append-only), got %d", len(rows))
	}
}

func TestMountOps_RegistersBothOps(t *testing.T) {
	m, _ := setup(t)
	reg := &mock.Router{}
	reg.Configure(mock.Config{Authorize: func(userID string, r model.Resource, a model.Action) bool { return true }})
	m.MountOps(reg)

	ctx := &mock.Context{InBody: []byte(`{"is_enabled":true,"changed_by":"u1","reason":"first"}`)}
	ctx.SetUserID("tester")
	reg.Invoke("OP", "/"+agentswitch.OpToggleAgentStatus, ctx)
	if ctx.Status != 0 {
		t.Fatalf("expected no error status, got %d: %s", ctx.Status, ctx.ResponseBody())
	}

	getCtx := &mock.Context{}
	getCtx.SetUserID("tester")
	reg.Invoke("OP", "/"+agentswitch.OpGetAgentStatus, getCtx)
	if getCtx.Status != 0 {
		t.Fatalf("expected no error status, got %d: %s", getCtx.Status, getCtx.ResponseBody())
	}

	deniedReg := &mock.Router{} // no Authorize configured (nil) => every guarded call is denied
	m.MountOps(deniedReg)
	deniedCtx := &mock.Context{}
	deniedCtx.SetUserID("someone")
	deniedReg.Invoke("OP", "/"+agentswitch.OpGetAgentStatus, deniedCtx)
	if deniedCtx.Status != 403 {
		t.Fatalf("expected 403 with no authorizer configured, got %d", deniedCtx.Status)
	}
}
```

`TestToggle_AppendOnly` calls `m.History()` — add this one small exported **domain** method to
`mcp.go` next to `GetStatus`/`Toggle`. Do NOT instead export a raw query-builder helper
(`func (m *Module) Query() *orm.QB` or similar): that leaks the ORM through the public API to serve
one test — an exported-plumbing / minimal-surface violation per `CONSTRUCTION_HARNESS.md`. A "list
the audit history" method is legitimate domain surface for an audit log, and doubles as the natural
backing method if a `list_toggle_history` op is ever added (§6):

```go
// History returns every audit row, newest first.
func (m *Module) History() ([]*AgentSwitch, error) {
	return ReadAllAgentSwitch(
		m.db.Query(&AgentSwitch{}).OrderBy(AgentSwitch_.ChangedAt).Desc(),
	)
}
```

Delete the current root-level `mcp_test.go` entirely once its logic is ported — it must not coexist
with the moved version.

## 9. Stage 8 — `go.mod` end state

Remove entirely (must not appear, direct or indirect, anywhere in `go.mod`/`go.sum` after `go mod
tidy`):

```
github.com/tinywasm/context
github.com/tinywasm/json
github.com/tinywasm/mcp
github.com/tinywasm/sqlite
github.com/tinywasm/unixid
```

Add / bump to (direct requires):

```
github.com/tinywasm/ddl     v0.0.7
github.com/tinywasm/events  v0.0.2
github.com/tinywasm/fmt     v0.25.5
github.com/tinywasm/form    v0.3.13
github.com/tinywasm/model   v0.1.2
github.com/tinywasm/orm     v0.11.4
github.com/tinywasm/router  v0.1.19
github.com/tinywasm/time    v0.5.2
```

(Re-verify against `item_catalog@main`'s `go.mod` at execution time — these move fast; the numbers
above are current as of this revision, not guaranteed current when this plan is dispatched.)

Run `go get` for each at the pinned version, then one `go mod tidy` at the module root — `tests/`
belongs to the same module (§8: no nested `go.mod`), so the same tidy resolves `storage/mem`,
`router/mock`, `events`, `orm` for the test-only imports. `github.com/tinywasm/storage` becomes a
**direct** require (its `storage/mem` package is imported by `tests/`); let `go mod tidy` place it.

## 10. Stage 9 — documentation consistency (small, mechanical)

Two docs describe the pre-harness behavior and go stale the moment Stage 1/2 land:

- `docs/diagrams/database.md`: currently says `id: string PK<br/>unixid — encodes timestamp` and a
  "Read strategy: `SELECT ... ORDER BY id DESC LIMIT 1`" note. Update the diagram to add the new
  `changed_at: int64 NOT NULL` node and change the read-strategy note to `ORDER BY changed_at DESC
  LIMIT 1`; drop "unixid — encodes timestamp" from the `id` node's label (identity is now generic,
  not `unixid`-specific).
- `README.md`: update the "MCP Tools" table heading/content to "Ops" (matching the rewritten
  `docs/ARCHITECTURE.md` from this same change), the "Constraints" section's mention of
  `db.CreateTable`/`unixid`/`uid.Parse`, and the "Quick Start" snippet to the new `New(db,
  Deps{...})` signature plus `m.MountOps(reg)` instead of `m.RegisterTools(srv)`.

Neither file is `.go`/`go.mod`, so both are in scope for this same PR.

## 11. Acceptance criteria

Run all of these from the module root after every stage above is complete:

- `grep -rn --include=*.go "tinywasm/mcp\|tinywasm/json\|tinywasm/unixid\|tinywasm/sqlite\|tinywasm/sqlt\|tinywasm/postgres\|tinywasm/layout\|tinywasm/context" .` → **empty**, repo-wide, tests included.
  Scoped to `.go` files deliberately: `tinywasm/json` legitimately reappears in `go.mod`/`go.sum` as
  an **indirect** entry (pulled transitively once `router`/`router/mock` are in the dependency graph)
  — the rule is about what this module's `.go` files import, not the transitive closure recorded in
  `go.sum`. Without `--include=*.go` this grep would find it there and false-fail even on a fully
  correct implementation.
- `grep -rn "db.CreateTable\|uid.Parse\|unixid\." .` → **empty**.
- `grep -rn "\"strings\"\|\"strconv\"\|\"errors\"" *.go tests/*.go` → **empty** (this module never
  used stdlib `fmt`, so no need to check for that one, but keep the check pattern in mind for any new
  code you add).
- `grep -rn "var _ router.OpModule = " mcp.go` → 1 match.
- `grep -rn "func (m \*Module) MountOps" mcp.go` → 1 match; `grep -rn "func (m \*Module) ModelName" mcp.go` → 1 match.
- `grep -rln "view.New\|NewView" *.go` → **empty** (Stage 5's judgment call: no view in this module).
- `go build ./...` and `GOOS=js GOARCH=wasm go build ./...` both clean from the module root
  (no file keeps `//go:build !wasm` — every remaining import is isomorphic).
- `find tests -name go.mod` → **empty** (`tests/` is part of the root module, no nested module, no
  `replace`).
- `gotest ./...` green from the module root (covers `tests/` too — same module).
- `go.mod`: none of `context`/`json`/`mcp`/`sqlite`/`unixid` present (direct or indirect); `ddl` is a
  direct require.
- `docs/diagrams/database.md` and `README.md` no longer mention `unixid` or `db.CreateTable`.

## 12. Out of scope — do not touch

- Do not rename `mcp.go` (§4).
- Do not add a "list toggle history" op or a `view.go` — Stage 5's decision stands unless a
  separate, later plan revisits it.
- Do not add back the `is_enabled`-presence check (§1b) by parsing the raw request body — that
  reintroduces a concrete-encoder dependency this migration removes.
- Do not touch anything under `docs/img/`.

## 13. Stages summary

| # | Stage | Output | Verifies with |
|---|---|---|---|
| 1 | `model.go` → `model.Definition`s (+ new `changed_at` field, §2) | 5 `Definition`s, `ErrChangedByRequired` | `ormc` regenerates cleanly; `model_orm.go` has `Id`/`ChangedAt` |
| 2 | `mcp.go`: `Deps`/`New`/service methods (§3) | `model.IDGenerator`+`events.Publisher` injected, `changed_at` set via `time.Now()` | compiles; no `unixid`/`context`/`json`/`mcp` imports |
| 3 | Import cleanup (§4) | trimmed import block | `grep` for removed imports empty |
| 4 | `router.OpModule`/`MountOps` (§5) | `ModelName`, `MountOps`, 2 `opXxx` handlers | `var _ router.OpModule = (*Module)(nil)` compiles |
| 5 | View decision (§6) | none — explicit no-op | `grep -l "view.New\|NewView"` empty |
| 6 | `ddl.CreateTable` in `New()` (§7) | schema migration behind `ddl.Compiler` type assertion | no `db.CreateTable` left |
| 7 | Tests → `tests/` (§8) | `tests/setup_test.go`, `tests/agent_switch_test.go` (same module, NO `tests/go.mod`); old `mcp_test.go` deleted | `gotest ./...` green |
| 8 | `go.mod` end state (§9) | deps added/removed as listed | `go.sum` has no `sqlite`/`unixid`/`mcp`/`json`/`context` |
| 9 | Docs consistency (§10) | `docs/diagrams/database.md`, `README.md` updated | no stale `unixid`/`db.CreateTable` mentions |
