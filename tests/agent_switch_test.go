package tests

import (
	"testing"

	"github.com/tinywasm/json"
	"github.com/tinywasm/model"
	"github.com/tinywasm/orm"
	"github.com/tinywasm/router/mock"
	"github.com/tinywasm/storage/mem"
	agentswitch "github.com/veltylabs/agent_switch"
)

func setup(t *testing.T) (*agentswitch.Module, *MockPublisher, *orm.DB) {
	t.Helper()
	db := orm.New(mem.New())
	pub := &MockPublisher{}
	m, err := agentswitch.New(db, agentswitch.Deps{IDs: &MockIDGen{}, Publisher: pub})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, pub, db
}

func TestGetStatus_NoHistory(t *testing.T) {
	m, _, _ := setup(t)
	row, err := m.GetStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row != nil {
		t.Fatalf("expected nil row for empty log, got %+v", row)
	}
}

func TestGetStatus_Enabled(t *testing.T) {
	m, _, _ := setup(t)
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
	m, _, _ := setup(t)
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
	m, pub, _ := setup(t)
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
	m, _, _ := setup(t)
	_, err := m.Toggle(agentswitch.ToggleArgs{IsEnabled: true})
	if err != agentswitch.ErrChangedByRequired {
		t.Fatalf("expected ErrChangedByRequired, got %v", err)
	}
}

func TestToggle_AppendOnly(t *testing.T) {
	m, _, _ := setup(t)
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
	m, _, _ := setup(t)
	if m.ModelName() != "agent_switch" {
		t.Fatalf("expected ModelName %q, got %q", "agent_switch", m.ModelName())
	}
	reg := &mock.Router{}
	reg.Configure(mock.Config{Authorize: func(userID string, r model.Resource, a model.Action) bool { return true }})
	m.MountOps(reg)

	ctx := &mock.Context{InBody: []byte(`{"is_enabled":true,"changed_by":"u1","reason":"first"}`)}
	ctx.SetUserID("tester")
	reg.Invoke("OP", "/"+agentswitch.OpToggleAgentStatus, ctx)
	if ctx.Status != 0 {
		t.Fatalf("expected no error status, got %d: %s", ctx.Status, ctx.ResponseBody())
	}

	// Decodifica la respuesta a través del codec real — prueba el contrato de wire de punta a
	// punta, no solo que el body no esté vacío. Ejercita ToggleResult.DecodeFields.
	var toggled agentswitch.ToggleResult
	if err := json.Decode(ctx.ResponseBody(), &toggled); err != nil {
		t.Fatalf("decode toggle response: %v", err)
	}
	if !toggled.Ok || !toggled.IsEnabled {
		t.Errorf("unexpected decoded toggle response: %+v", toggled)
	}

	getCtx := &mock.Context{}
	getCtx.SetUserID("tester")
	reg.Invoke("OP", "/"+agentswitch.OpGetAgentStatus, getCtx)
	if getCtx.Status != 0 {
		t.Fatalf("expected no error status, got %d: %s", getCtx.Status, getCtx.ResponseBody())
	}

	// Mismo roundtrip real para StatusResult — ejercita su DecodeFields, hoy sin cubrir.
	var status agentswitch.StatusResult
	if err := json.Decode(getCtx.ResponseBody(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if !status.IsEnabled || status.ChangedBy != "u1" || status.Reason != "first" {
		t.Errorf("unexpected decoded status response: %+v", status)
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

func TestNew_RequiresIDs(t *testing.T) {
	db := orm.New(mem.New())
	if _, err := agentswitch.New(db, agentswitch.Deps{}); err == nil {
		t.Fatal("expected an error when Deps.IDs is nil")
	}
}

func TestMountOps_GetAgentStatus_Empty(t *testing.T) {
	m, _, _ := setup(t) // sin ningún Toggle previo — el registro está vacío
	reg := &mock.Router{}
	reg.Configure(mock.Config{Authorize: func(userID string, r model.Resource, a model.Action) bool { return true }})
	m.MountOps(reg)

	ctx := &mock.Context{}
	ctx.SetUserID("tester")
	reg.Invoke("OP", "/"+agentswitch.OpGetAgentStatus, ctx)
	if ctx.Status != 0 {
		t.Fatalf("expected no error status for an empty log, got %d: %s", ctx.Status, ctx.ResponseBody())
	}
	var empty agentswitch.StatusEmptyResult
	if err := json.Decode(ctx.ResponseBody(), &empty); err != nil {
		t.Fatalf("decode empty status response: %v", err)
	}
}

func TestMountOps_ToggleAgentStatus_MissingChangedBy(t *testing.T) {
	m, _, _ := setup(t)
	reg := &mock.Router{}
	reg.Configure(mock.Config{Authorize: func(userID string, r model.Resource, a model.Action) bool { return true }})
	m.MountOps(reg)

	ctx := &mock.Context{InBody: []byte(`{"is_enabled":true}`)}
	ctx.SetUserID("tester")
	reg.Invoke("OP", "/"+agentswitch.OpToggleAgentStatus, ctx)
	if ctx.Status != 400 {
		t.Fatalf("expected 400 for missing changed_by, got %d", ctx.Status)
	}
}

func TestReadOneAgentSwitch(t *testing.T) {
	m, _, db := setup(t)
	// Consulta db vacía
	var empty agentswitch.AgentSwitch
	_, err := agentswitch.ReadOneAgentSwitch(db.Query(&empty), &empty)
	if err == nil {
		t.Fatalf("expected error on empty db ReadOneAgentSwitch")
	}

	// Alternar una fila
	if _, err := m.Toggle(agentswitch.ToggleArgs{IsEnabled: true, ChangedBy: "u1"}); err != nil {
		t.Fatalf("Toggle: %v", err)
	}

	var row agentswitch.AgentSwitch
	got, err := agentswitch.ReadOneAgentSwitch(db.Query(&row).Limit(1), &row)
	if err != nil {
		t.Fatalf("ReadOneAgentSwitch: %v", err)
	}
	if got == nil || got.ChangedBy != "u1" {
		t.Fatalf("expected row with changed_by 'u1', got %+v", got)
	}
}
