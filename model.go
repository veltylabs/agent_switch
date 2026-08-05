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
