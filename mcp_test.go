//go:build !wasm

package agentswitch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tinywasm/context"
	"github.com/tinywasm/mcp"
	"github.com/tinywasm/sqlite"
)

func setupTestModule(t *testing.T) *Module {
	db, _ := sqlite.Open(":memory:")
	m, _ := New(db)
	return m
}

func TestTools(t *testing.T) {
	m := setupTestModule(t)
	tools := m.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	if tools[0].Name != "get_agent_status" {
		t.Errorf("expected tool 0 to be get_agent_status, got %s", tools[0].Name)
	}

	if tools[1].Name != "toggle_agent_status" {
		t.Errorf("expected tool 1 to be toggle_agent_status, got %s", tools[1].Name)
	}

	if tools[1].Resource != "agent_switch" {
		t.Errorf("expected resource agent_switch, got %s", tools[1].Resource)
	}
}

func TestGetAgentStatus_Enabled(t *testing.T) {
	m := setupTestModule(t)
	m.db.Create(&AgentSwitch{
		ID:        m.uid.NewID(),
		IsEnabled: true,
		ChangedBy: "u1",
		Reason:    "test",
	})

	var ctx context.Context
	res, err := m.GetStatus(&ctx, mcp.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected no error in result, got %s", res.Content)
	}

	text, err := mcp.GetText(res)
	if err != nil {
		t.Fatalf("failed to get text: %v", err)
	}

	var mRes map[string]any
	if err := json.Unmarshal([]byte(text), &mRes); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if mRes["is_enabled"] != true {
		t.Errorf("expected is_enabled to be true, got %v", mRes["is_enabled"])
	}
	if mRes["changed_by"] != "u1" {
		t.Errorf("expected changed_by to be 'u1', got %v", mRes["changed_by"])
	}
}

func TestGetAgentStatus_NoHistory(t *testing.T) {
	m := setupTestModule(t)

	var ctx context.Context
	res, err := m.GetStatus(&ctx, mcp.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected no error in result, got %s", res.Content)
	}

	text, err := mcp.GetText(res)
	if err != nil {
		t.Fatalf("failed to get text: %v", err)
	}

	var mRes map[string]any
	if err := json.Unmarshal([]byte(text), &mRes); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if mRes["is_enabled"] != false {
		t.Errorf("expected is_enabled to be false, got %v", mRes["is_enabled"])
	}
	if mRes["changed_at"].(float64) != 0 {
		t.Errorf("expected changed_at to be 0, got %v", mRes["changed_at"])
	}
}

func TestGetAgentStatus_ReturnsLatestOnly(t *testing.T) {
	m := setupTestModule(t)
	m.db.Create(&AgentSwitch{
		ID:        m.uid.NewID(),
		IsEnabled: true,
		ChangedBy: "u1",
		Reason:    "1",
	})
	m.db.Create(&AgentSwitch{
		ID:        m.uid.NewID(),
		IsEnabled: true,
		ChangedBy: "u2",
		Reason:    "2",
	})
	m.db.Create(&AgentSwitch{
		ID:        m.uid.NewID(),
		IsEnabled: false,
		ChangedBy: "u3",
		Reason:    "3",
	})

	var ctx context.Context
	res, err := m.GetStatus(&ctx, mcp.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected no error in result, got %s", res.Content)
	}

	text, err := mcp.GetText(res)
	if err != nil {
		t.Fatalf("failed to get text: %v", err)
	}

	var mRes map[string]any
	if err := json.Unmarshal([]byte(text), &mRes); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if mRes["is_enabled"] != false {
		t.Errorf("expected is_enabled to be false, got %v", mRes["is_enabled"])
	}
	if mRes["changed_by"] != "u3" {
		t.Errorf("expected changed_by to be 'u3', got %v", mRes["changed_by"])
	}
}

func TestToggleAgentStatus_Enable(t *testing.T) {
	m := setupTestModule(t)

	var ctx context.Context
	req := mcp.Request{}
	req.Params.Arguments = `{"is_enabled": true, "changed_by": "u1", "reason": "test"}`

	res, err := m.Toggle(&ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected no error in result, got %s", res.Content)
	}

	text, err := mcp.GetText(res)
	if err != nil {
		t.Fatalf("failed to get text: %v", err)
	}

	var mRes map[string]any
	if err := json.Unmarshal([]byte(text), &mRes); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if mRes["ok"] != true {
		t.Errorf("expected ok to be true, got %v", mRes["ok"])
	}
	if mRes["is_enabled"] != true {
		t.Errorf("expected is_enabled to be true, got %v", mRes["is_enabled"])
	}

	rows, err := ReadAllAgentSwitch(m.db.Query(&AgentSwitch{}))
	if err != nil {
		t.Fatalf("unexpected error querying db: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row in DB, got %d", len(rows))
	}
	if rows[0].IsEnabled != true {
		t.Errorf("expected DB row to be enabled")
	}
	if rows[0].ChangedBy != "u1" {
		t.Errorf("expected DB row changed_by to be u1, got %v", rows[0].ChangedBy)
	}
}

func TestToggleAgentStatus_MissingIsEnabled(t *testing.T) {
	m := setupTestModule(t)

	var ctx context.Context
	req := mcp.Request{}
	req.Params.Arguments = `{"changed_by": "u1"}`

	res, err := m.Toggle(&ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result, got success")
	}
	if !strings.Contains(strings.ToLower(res.Content), "params invalid") && !strings.Contains(strings.ToLower(res.Content), "invalid") {
		t.Errorf("expected params invalid error, got %v", res.Content)
	}
}

func TestToggleAgentStatus_MissingChangedBy(t *testing.T) {
	m := setupTestModule(t)

	var ctx context.Context
	req := mcp.Request{}
	req.Params.Arguments = `{"is_enabled": true}`

	res, err := m.Toggle(&ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result, got success")
	}
	if !strings.Contains(strings.ToLower(res.Content), "params invalid") && !strings.Contains(strings.ToLower(res.Content), "invalid") {
		t.Errorf("expected params invalid error, got %v", res.Content)
	}
}

func TestToggleAgentStatus_AppendOnly(t *testing.T) {
	m := setupTestModule(t)

	var ctx context.Context
	req1 := mcp.Request{}
	req1.Params.Arguments = `{"is_enabled": true, "changed_by": "u1"}`
	_, err := m.Toggle(&ctx, req1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req2 := mcp.Request{}
	req2.Params.Arguments = `{"is_enabled": false, "changed_by": "u2"}`
	_, err = m.Toggle(&ctx, req2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows, err := ReadAllAgentSwitch(m.db.Query(&AgentSwitch{}))
	if err != nil {
		t.Fatalf("unexpected error querying db: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows in DB, got %d", len(rows))
	}
}
