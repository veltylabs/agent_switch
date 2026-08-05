package agentswitch

import (
	"github.com/tinywasm/ddl"
	"github.com/tinywasm/events"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/model"
	"github.com/tinywasm/orm"
	"github.com/tinywasm/router"
	"github.com/tinywasm/time"
)

const (
	OpGetAgentStatus    = "get_agent_status"
	OpToggleAgentStatus = "toggle_agent_status"

	TopicAgentToggled = "agent_switch.toggled"
)

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

// History returns every audit row, newest first.
func (m *Module) History() ([]*AgentSwitch, error) {
	return ReadAllAgentSwitch(
		m.db.Query(&AgentSwitch{}).OrderBy(AgentSwitch_.ChangedAt).Desc(),
	)
}

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
