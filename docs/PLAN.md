# PLAN — agent_switch: migrar model.go a model.Definition

> This plan is dispatched via the CodeJob workflow. See skill: **agents-workflow**.

✅ **Desbloqueado.** `github.com/tinywasm/model@v0.0.14` (con `orm@v0.9.28`) ya lee `model.Definition`.
`go get github.com/tinywasm/model@v0.0.14 github.com/tinywasm/orm@v0.9.28` antes de regenerar.
⚠️ **Casing puro:** el campo `id` genera `Id` (no `ID`); actualiza referencias `.ID`→`.Id` en
consumidores (ver §5).

Eres un agente **sin contexto previo** y **solo tienes este repositorio** (`agent_switch`). Este plan
es autocontenido: todo contrato, regla y ejemplo está inline.

---

## 1. Qué cambia y por qué

El ecosistema tinywasm invirtió la forma de definir modelos. **Hoy** escribes un struct Go + tags
string y `ormc` genera el schema (`model_orm.go`). **Ahora** escribes la definición **tipada** a mano
(`model.Definition`) y `ormc` genera el struct concreto + toda la plomería. Es una migración
**mecánica**: mismo comportamiento observable, mismos nombres de columna/tabla, mismo JSON — solo
cambia cómo se autora el schema.

## 2. Contrato de `github.com/tinywasm/model` (inline)

`Field.Type` **no** es un literal de un enum — es la interfaz `Kind`. Se rellena llamando a un
constructor (`model.Text()`, `model.Int()`, …), nunca asignando `model.FieldText` directamente:

```go
package model

// FieldType es el mapeo determinista de almacenamiento/wire — lo devuelve Kind.Storage(),
// nunca se asigna directamente a Field.Type.
type FieldType int
const (
    FieldText FieldType = iota // string
    FieldInt                   // int64
    FieldFloat                 // float64
    FieldBool                  // bool
    FieldBlob                  // []byte
    FieldStruct                // struct anidado — Kind = model.Struct(ref)
    FieldIntSlice               // []int
    FieldStructSlice            // []T anidado — Kind = model.StructSlice(ref)
    FieldRaw                    // JSON pre-serializado
)

// Kind reemplaza el antiguo par Field.Type-enum + Field.Widget. Implementaciones
// sin estado, seguras para reuso concurrente.
type Kind interface {
    Storage() FieldType          // mapeo determinista a Go/DDL
    Name() string                // nombre semántico: "text", "int", "email", ...
    Validate(value string) error // SIEMPRE presente — fail-closed
}

// Constructores base — devuelven Kind, no un literal FieldType:
func Text() Kind  // storage FieldText
func Int() Kind   // storage FieldInt
func Float() Kind // storage FieldFloat
func Bool() Kind  // storage FieldBool
func Blob() Kind  // storage FieldBlob

type FieldDB struct { PK, Unique, AutoInc bool }

type Field struct {
    Name      string      // nombre snake_case en wire/DB
    Type      Kind        // model.Text(), model.Int(), ... — NUNCA un literal FieldType
    NotNull   bool
    OmitEmpty bool
    DB        *FieldDB    // nil = campo transporte/form-only (sin persistencia)
    Ref       *Definition // solo FK escalar; en composición (Struct/StructSlice) el ref va
                          // en el constructor del Kind, no aquí
    Exclude   bool        // campo en el struct generado, pero fuera de Pointers/codec
    Permitted             // reglas de validación (no se usa en este módulo)
}

type Fields = []Field

type Definition struct {
    Name   string // identidad: nombre de tabla / ModelName()
    Fields Fields
}
```

Tipos Go soportados — **mapeo fijo, no hay más tipos**: `model.Text()`→`string`, `model.Int()`→`int64`,
`model.Float()`→`float64`, `model.Bool()`→`bool`, `model.Blob()`→`[]byte`.

**Convención de nombre:** la variable debe llamarse `<Struct>Model` (ej. `AgentSwitchModel` → genera
`type AgentSwitch struct`).

**Structs sin rol DB** (antes `// ormc:formonly`): todos sus `Field` tienen `DB: nil`.

**Ya no existe `Field.Widget`.** Un Kind con UI es un `Kind` de `github.com/tinywasm/form/input`
(p. ej. `input.Text()`), que también implementa `Storage()`/`Name()`/`Validate()`. Este módulo
**sí** usa widgets hoy — en 4 de sus 5 `Definition` (ver §4) — así que no basta con los Kinds
base para todo el archivo.

---

## 3. Estado actual (`model.go`, a portar)

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

## 4. Estado objetivo (`model.go` reescrito)

Preserva el build tag `//go:build !wasm` (ya presente hoy) y el nombre de paquete `agentswitch`.

```go
//go:build !wasm

package agentswitch

import (
	"github.com/tinywasm/form/input"
	"github.com/tinywasm/model"
)

// AgentSwitchModel: sin widgets — el `model_orm.go` ACTUAL tampoco los tiene en
// ninguno de sus campos (no hay UI que edite este registro directamente hoy).
// No inventes widgets aquí: preserva ese estado.
var AgentSwitchModel = model.Definition{
	Name: "agent_switch",
	Fields: model.Fields{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "is_enabled", Type: model.Bool(), NotNull: true},
		{Name: "changed_by", Type: model.Text(), NotNull: true},
		{Name: "reason", Type: model.Text()},
	},
}

// Los 4 structs de abajo SÍ tienen widget hoy en el `model_orm.go` actual
// (campo `Widget:` de la API vieja) — son los args/resultados de los tools MCP
// de este módulo, y algo los renderiza. Preserva esa asignación exacta con
// `input.X()`; no la dejes caer o el form saldría vacío en silencio.

var StatusEmptyResultModel = model.Definition{
	Name: "status_empty_result",
	Fields: model.Fields{
		{Name: "is_enabled", Type: input.Checkbox()},
		{Name: "changed_at", Type: input.Number()},
	},
}

var StatusResultModel = model.Definition{
	Name: "status_result",
	Fields: model.Fields{
		{Name: "is_enabled", Type: input.Checkbox()},
		{Name: "changed_by", Type: input.Text()},
		{Name: "changed_at", Type: input.Number()},
		{Name: "reason", Type: input.Text()},
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
		{Name: "ok", Type: input.Checkbox()},
		{Name: "is_enabled", Type: input.Checkbox()},
	},
}
```

Nota: `AgentSwitch.ID` **no** lleva `AutoInc` — hoy el comentario dice "set by caller via unixid",
consistente con `DB: &model.FieldDB{PK: true}` sin `AutoInc`. No cambia.

**Por qué estos 4 sí y `AgentSwitchModel` no:** verificado contra el `model_orm.go` que este
mismo repo tiene generado *hoy* (API vieja, con un campo `Widget:` separado): `AgentSwitchModel`
no tiene ningún `Widget:` asignado en ninguno de sus campos, pero `StatusEmptyResultModel`,
`StatusResultModel`, `ToggleArgsModel` y `ToggleResultModel` sí, campo por campo, exactamente
como quedó arriba. Si al migrar se dejan como Kinds base (`model.Bool()`/`model.Text()`/
`model.Int()`) sin widget, cualquier `form.New()` construido sobre ellos saldría **vacío** —
el mismo defecto que ya se detectó y corrigió en `service_catalog`.

## 5. Pasos

> **Dependencias:** `go get github.com/tinywasm/model@v0.0.14 github.com/tinywasm/orm@v0.9.28 github.com/tinywasm/form@v0.2.15`
> (`model` directa nueva, antes solo se llegaba transitivamente vía `orm`; `form` ya era
> dependencia directa — solo se bumpea para tener `input.Decimal()` disponible aunque este
> módulo no lo necesite hoy).

1. Reescribe `model.go` con el contenido de §4 (elimina los structs planos + tags; ya no se escriben
   a mano — los genera `ormc`). **Sin directivas** (`// ormc:formonly`/`// orm:typed_fields` ya no
   existen; el rol codec-only se infiere de `DB: nil` en todos los campos).
2. Corre el generador `ormc` (instalado/actual) para regenerar `model_orm.go`. El struct `AgentSwitch`
   resultante tiene `IsEnabled bool`, `ChangedBy string`, `Reason string` — y ⚠️ **`Id string`** (el
   campo `id` genera `Id` con casing puro, **no** `ID`). Los formonly (`statusEmptyResult`, etc.) igual,
   con `ChangedAt int64`.
3. Ajusta `mcp.go`/`*_test.go`: los **nombres de tipo** Go no cambian (`AgentSwitch`, …), pero el
   **campo** `.ID`→`.Id` sí — actualiza toda referencia (`sw.ID`→`sw.Id`, etc.).
4. Verifica que `db.Create`/`Query` sigan funcionando igual (mismo `ModelName()` → `"agent_switch"`,
   mismas columnas; el JSON/wire no cambia).

## 6. Fuera de alcance

- No renombrar tipos, columnas, ni cambiar comportamiento del módulo.
- No añadir widgets **nuevos** que no tuviera ya el `model_orm.go` actual (no le pongas widget a
  `AgentSwitchModel`: hoy no lo tiene). Sí **preservar** los 4 que ya existen (§4) — omitirlos no
  es "no añadir", es dejar caer algo que ya estaba y romper el form en silencio.
- No tocar la lógica de negocio (`mcp.go` u otros) salvo lo estrictamente necesario para compilar.

## 7. Criterio de aceptación

- `gotest ./...` verde con `go.mod` en `model v0.0.14` / `orm v0.9.28`.
- `model_orm.go` regenerado compila; mismos tipos y métodos, con el campo `Id` (casing puro, antes `ID`)
  actualizado en todos los consumidores. Columnas/JSON sin cambios.
- `model.go` no contiene structs planos con tags `db:` ni directivas; solo `var ...Model = model.Definition{...}`.
- `StatusEmptyResultModel`, `StatusResultModel`, `ToggleArgsModel`, `ToggleResultModel` conservan
  sus widgets (`input.Checkbox()`/`input.Text()`/`input.Number()`, ver §4); `AgentSwitchModel`
  sigue sin ninguno.

## 8. Etapas

| # | Etapa | Salida | Criterio |
|---|---|---|---|
| 1 | Reescribir `model.go` | Definitions de §4 (4 structs conservan sus widgets `input.X()`; `AgentSwitchModel` sin widget) | compila (con ormc actualizado) |
| 2 | Regenerar `model_orm.go` | struct + plomería | misma forma que hoy |
| 3 | Ajustar consumidores | `mcp.go`/tests si aplica | `gotest ./...` verde |
