package tests

import (
	"testing"

	agentswitch "github.com/veltylabs/agent_switch"
	"github.com/tinywasm/model"
)

type DummyFieldWriter struct{}

func (DummyFieldWriter) String(name, val string) {}
func (DummyFieldWriter) Int(name string, val int64) {}
func (DummyFieldWriter) Float(name string, val float64) {}
func (DummyFieldWriter) Bool(name string, val bool) {}
func (DummyFieldWriter) Bytes(name string, val []byte) {}
func (DummyFieldWriter) Null(name string) {}
func (DummyFieldWriter) Raw(name, val string) {}
func (DummyFieldWriter) Object(name string, val model.Encodable) {}
func (DummyFieldWriter) Array(name string, n int) model.ArrayWriter { return nil }

type DummyFieldReader struct{}

func (DummyFieldReader) String(name string) (string, bool) { return "test", true }
func (DummyFieldReader) Int(name string) (int64, bool) { return 42, true }
func (DummyFieldReader) Float(name string) (float64, bool) { return 3.14, true }
func (DummyFieldReader) Bool(name string) (bool, bool) { return true, true }
func (DummyFieldReader) Bytes(name string) ([]byte, bool) { return []byte("bytes"), true }
func (DummyFieldReader) Object(name string, into model.Decodable) bool { return true }
func (DummyFieldReader) Array(name string) (model.ArrayReader, bool) { return nil, true }
func (DummyFieldReader) Raw(name string) (string, bool) { return "raw", true }

func TestModelAndOrmCoverage(t *testing.T) {
	w := DummyFieldWriter{}
	r := DummyFieldReader{}

	// Cobertura para ModelName() en el módulo mismo
	m, _, _ := setup(t)
	if name := m.ModelName(); name != "agent_switch" {
		t.Errorf("expected module ModelName 'agent_switch', got %s", name)
	}

	// 1. AgentSwitch
	{
		var val agentswitch.AgentSwitch
		_ = val.ModelName()
		_ = val.Schema()
		_ = val.Pointers()
		_ = val.IsNil()
		val.EncodeFields(w)
		val.DecodeFields(r)
		_ = val.Validate(model.ActionCreate)

		var list agentswitch.AgentSwitchList
		_ = list.Schema()
		_ = list.Pointers()
		_ = list.Len()
		list.Append()
		_ = list.Len()
		_ = list.At(0)
		_ = list.IsNil()
		list.EncodeFields(w)
		list.DecodeFields(r)
	}

	// 2. StatusEmptyResult
	{
		var val agentswitch.StatusEmptyResult
		_ = val.ModelName()
		_ = val.Schema()
		_ = val.Pointers()
		_ = val.IsNil()
		val.EncodeFields(w)
		val.DecodeFields(r)
		_ = val.Validate(model.ActionCreate)

		var list agentswitch.StatusEmptyResultList
		_ = list.Schema()
		_ = list.Pointers()
		_ = list.Len()
		list.Append()
		_ = list.Len()
		_ = list.At(0)
		_ = list.IsNil()
		list.EncodeFields(w)
		list.DecodeFields(r)
	}

	// 3. StatusResult
	{
		var val agentswitch.StatusResult
		_ = val.ModelName()
		_ = val.Schema()
		_ = val.Pointers()
		_ = val.IsNil()
		val.EncodeFields(w)
		val.DecodeFields(r)
		_ = val.Validate(model.ActionCreate)

		var list agentswitch.StatusResultList
		_ = list.Schema()
		_ = list.Pointers()
		_ = list.Len()
		list.Append()
		_ = list.Len()
		_ = list.At(0)
		_ = list.IsNil()
		list.EncodeFields(w)
		list.DecodeFields(r)
	}

	// 4. ToggleArgs
	{
		var val agentswitch.ToggleArgs
		_ = val.ModelName()
		_ = val.Schema()
		_ = val.Pointers()
		_ = val.IsNil()
		val.EncodeFields(w)
		val.DecodeFields(r)
		_ = val.Validate(model.ActionCreate)

		var list agentswitch.ToggleArgsList
		_ = list.Schema()
		_ = list.Pointers()
		_ = list.Len()
		list.Append()
		_ = list.Len()
		_ = list.At(0)
		_ = list.IsNil()
		list.EncodeFields(w)
		list.DecodeFields(r)
	}

	// 5. ToggleResult
	{
		var val agentswitch.ToggleResult
		_ = val.ModelName()
		_ = val.Schema()
		_ = val.Pointers()
		_ = val.IsNil()
		val.EncodeFields(w)
		val.DecodeFields(r)
		_ = val.Validate(model.ActionCreate)

		var list agentswitch.ToggleResultList
		_ = list.Schema()
		_ = list.Pointers()
		_ = list.Len()
		list.Append()
		_ = list.Len()
		_ = list.At(0)
		_ = list.IsNil()
		list.EncodeFields(w)
		list.DecodeFields(r)
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
