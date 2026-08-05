//go:build !wasm

package agentswitch

import (
	"strings"

	"github.com/tinywasm/context"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/json"
	"github.com/tinywasm/mcp"
	"github.com/tinywasm/orm"
	"github.com/tinywasm/unixid"
)

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

// GetStatus returns the current agent enabled/disabled state.
func (m *Module) GetStatus(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
	rows, err := ReadAllAgentSwitch(
		m.db.Query(&AgentSwitch{}).OrderBy(AgentSwitch_.ID).Desc().Limit(1),
	)
	if err != nil {
		return &mcp.Result{IsError: true, Content: fmt.Err("database", "unavailable").Error()}, nil
	}
	if len(rows) == 0 {
		var out string
		_ = json.Encode(&statusEmptyResult{}, &out)
		return mcp.Text(out), nil
	}
	r := rows[0]

	// Extract timestamp from unixid
	ts, _, err := m.uid.Parse(r.ID)
	if err != nil {
		return &mcp.Result{IsError: true, Content: err.Error()}, nil
	}

	var out string
	_ = json.Encode(&statusResult{
		IsEnabled: r.IsEnabled,
		ChangedBy: r.ChangedBy,
		ChangedAt: ts,
		Reason:    r.Reason,
	}, &out)
	return mcp.Text(out), nil
}

// Toggle inserts a new audit row. Append-only — never updates existing rows.
func (m *Module) Toggle(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
	if !strings.Contains(req.Params.Arguments, "is_enabled") {
		return &mcp.Result{IsError: true, Content: fmt.Err("params", "invalid").Error()}, nil
	}

	var args toggleArgs
	if err := json.Decode(req.Params.Arguments, &args); err != nil || args.ChangedBy == "" {
		return &mcp.Result{IsError: true, Content: fmt.Err("params", "invalid").Error()}, nil
	}

	row := &AgentSwitch{
		ID:        m.uid.NewID(),
		IsEnabled: args.IsEnabled,
		ChangedBy: args.ChangedBy,
		Reason:    args.Reason,
	}
	if err := m.db.Create(row); err != nil {
		return &mcp.Result{IsError: true, Content: fmt.Err("database", "unavailable").Error()}, nil
	}

	var out string
	_ = json.Encode(&toggleResult{OK: true, IsEnabled: args.IsEnabled}, &out)
	return mcp.Text(out), nil
}
