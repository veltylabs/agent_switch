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
