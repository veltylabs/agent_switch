package agentswitch

import (
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/form/input"
	"github.com/tinywasm/model"
)

var ErrChangedByRequired = fmt.Err("changed_by is required")

// AgentSwitchModel: fila de auditoría de solo inserción (append-only). Sin UPDATE, sin DELETE — ver AGENTS.md
// "Domain-specific notes". Sin widget en ningún campo: nada renderiza este registro
// directamente (igual que el model_orm.go generado anteriormente, que no tiene Widget: en ningún
// de los campos de AgentSwitch — no agregar uno aquí).
//
// changed_at es una NUEVA columna (not present en la versión de struct+tags que reemplaza).
// Reemplaza la derivación de la marca de tiempo a partir del id (específico de unixid; model.IDGenerator
// expone solo NewID() string) — ver docs/PLAN.md §1a para la justificación completa. Se establece en el
// momento de la inserción mediante github.com/tinywasm/time.Now() (Etapa 3); se lee mediante
// ORDER BY changed_at DESC LIMIT 1 (Etapa 5), no ORDER BY id DESC.
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

// Las 4 definiciones de abajo son de solo transporte (DB: nil en cada campo, implícitamente —
// nunca se establece DB) — argumentos/resultados de las dos operaciones de este módulo. Política de widgets (ver
// la nota arriba del archivo de destino §2): ToggleArgsModel es el único registro editable por el usuario,
// por lo que solo él lleva widgets de entrada; los tres modelos de resultado son de solo salida y usan
// tipos base — un resultado nunca debe renderizarse como un formulario editable.

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
