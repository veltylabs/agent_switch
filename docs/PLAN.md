---
PLAN: "test: agent_switch replace coverage-padding test with real-value tests"
TAG: v0.1.1
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 3091233685175467547
---

> This plan is dispatched via the CodeJob workflow. See skill: **agents-workflow**.

# PLAN — agent_switch: quitar el test de relleno, agregar cobertura de valor real

Eres un agente **sin contexto previo** y **solo tienes este repositorio** (`agent_switch`). El
módulo ya adoptó el patrón del arnés reutilizable en una ronda anterior; este plan es un ajuste
pequeño y autocontenido sobre ese trabajo ya mergeado.

## 1. Por qué existe este plan

Una ronda anterior (en respuesta a un comentario de revisión pidiendo más cobertura) agregó
`tests/coverage_test.go` con `TestModelAndOrmCoverage`: una función que llama **todos** los métodos
generados de **todos** los tipos de transporte (`ModelName`/`Schema`/`Pointers`/`IsNil`/
`EncodeFields`/`DecodeFields`/`Validate`, más las variantes de lista) contra un `DummyFieldWriter`/
`DummyFieldReader` hechos a mano, descartando cada valor de retorno (`_ = val.Schema()`, etc.). Esto
subió el número reportado a 88.4%, pero **no prueba ningún comportamiento real** — es exactamente el
antipatrón de "cobertura inflada" que este ecosistema prohíbe (`AGENTS.md`, sección Testing: *"a test
that calls methods and discards every return... inflates coverage while proving nothing"*).

**Este plan elimina esa función y la reemplaza con pruebas que sí aportan valor.** La cobertura
honesta resultante es ~48.8% (medida con `go test ./tests/... -coverpkg=github.com/veltylabs/agent_switch
-cover`, sin la función de relleno) — por debajo de 80%, pero §3 explica por qué ese es el techo
razonable aquí, igual que en `business_hours`/`work_schedule`.

## 2. Eliminar `TestModelAndOrmCoverage` — conservar `TestReadOneAgentSwitch`

`tests/coverage_test.go` tiene **dos** funciones: `TestModelAndOrmCoverage` (la de relleno, arriba)
y `TestReadOneAgentSwitch` (legítima — sí hace aserciones reales sobre el comportamiento de
`ReadOneAgentSwitch`, incluyendo el caso de error con DB vacía).

1. **Mueve `TestReadOneAgentSwitch`** (función completa, sin cambios) a `tests/agent_switch_test.go`,
   al final del archivo.
2. **Borra `tests/coverage_test.go` por completo** — incluyendo `DummyFieldWriter`, `DummyFieldReader`
   y `TestModelAndOrmCoverage`. Ninguno de los tres sobrevive.

## 3. Analizado — por qué ~49% es el techo razonable aquí, no 80%

Igual que en `work_schedule`, alrededor de la mitad de los métodos generados por `ormc` para los 5
`Definition`s de este módulo (`AgentSwitch`, `StatusEmptyResult`, `StatusResult`, `ToggleArgs`,
`ToggleResult`) **nunca se invocan por ningún camino real**:

- `AgentSwitch.EncodeFields`/`DecodeFields`/`Validate` — este tipo nunca viaja directamente por el
  wire (los ops construyen `StatusResult`/`ToggleResult` a partir de él, nunca lo codifican
  directamente), y nada en este módulo hace `Create`/`Update` a través de una validación explícita
  del registro (`Toggle` inserta directamente).
- `ModelName()`/`Schema()`/`Pointers()` en los 4 tipos de transporte — el codec JSON usa
  `EncodeFields`/`DecodeFields` directamente, nunca estos tres; solo importarían si este módulo
  construyera un `form.New(...)` sobre alguno de ellos, lo cual no hace.
- Las 5 variantes `XxxList` (`AgentSwitchList`, `StatusEmptyResultList`, etc.) — ninguna se usa como
  lista de nivel superior en este módulo.

**No escribas pruebas para cerrar estos huecos llamando a los métodos directamente** — eso es
exactamente lo que `TestModelAndOrmCoverage` hacía mal. Si una ronda futura decide que el objetivo de
cobertura debe aplicarse literalmente incluso aquí, es una decisión de alcance distinta.

## 4. Las pruebas de valor real a agregar — en `tests/agent_switch_test.go`

### 4.1 — Extender `TestMountOps_RegistersBothOps`

Al inicio de la función, justo después de `m, _, _ := setup(t)`, agrega:

```go
	if m.ModelName() != "agent_switch" {
		t.Fatalf("expected ModelName %q, got %q", "agent_switch", m.ModelName())
	}
```

Después del bloque que verifica el `toggle_agent_status` exitoso (`if ctx.Status != 0 {...}`),
agrega (requiere importar `"github.com/tinywasm/json"` — ver §5):

```go
	// Decodifica la respuesta a través del codec real — prueba el contrato de wire de punta a
	// punta, no solo que el body no esté vacío. Ejercita ToggleResult.DecodeFields.
	var toggled agentswitch.ToggleResult
	if err := json.Decode(ctx.ResponseBody(), &toggled); err != nil {
		t.Fatalf("decode toggle response: %v", err)
	}
	if !toggled.Ok || !toggled.IsEnabled {
		t.Errorf("unexpected decoded toggle response: %+v", toggled)
	}
```

Después del bloque que verifica el `get_agent_status` exitoso (`if getCtx.Status != 0 {...}`),
agrega:

```go
	// Mismo roundtrip real para StatusResult — ejercita su DecodeFields, hoy sin cubrir.
	var status agentswitch.StatusResult
	if err := json.Decode(getCtx.ResponseBody(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if !status.IsEnabled || status.ChangedBy != "u1" || status.Reason != "first" {
		t.Errorf("unexpected decoded status response: %+v", status)
	}
```

### 4.2 — Tres funciones nuevas, agregadas después de `TestMountOps_RegistersBothOps`

```go
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
```

## 5. Import a agregar

En `tests/agent_switch_test.go`, agrega `"github.com/tinywasm/json"` al bloque de imports existente
(junto a `agentswitch`, `model`, `orm`, `router/mock`, `storage/mem`). Corre `go mod tidy` después —
la excepción documentada en `AGENTS.md` (*"in tests, use `github.com/tinywasm/json` for codec
verification"*).

## 6. Fuera de alcance

- No tocar `mcp.go`, `model.go`, `model_orm.go` — ya están correctos (comentarios en español desde
  la ronda anterior).
- No agregar pruebas para lo descartado en §3.
- No perseguir 80% — ~49% es el resultado esperado de aplicar exactamente §2+§4.

## 7. Criterio de aceptación

- `test -f tests/coverage_test.go` → no existe.
- `grep -rn "TestModelAndOrmCoverage\|DummyFieldWriter\|DummyFieldReader" .` → vacío.
- `grep -n "func TestReadOneAgentSwitch" tests/agent_switch_test.go` → 1 match.
- `go build ./...` y `GOOS=js GOARCH=wasm go build ./...` limpios.
- `gotest ./...` verde.
- `go test ./tests/... -coverpkg=github.com/veltylabs/agent_switch -cover` reporta ~49% (no 88%
  inflado — ver §1/§3).
- `git status` limpio tras el commit.
