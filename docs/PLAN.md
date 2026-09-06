---
PLAN: "refactor!: webtyp.com rename + move webtyp.com/form/input → webtyp.com/input"
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# Plan — `agent_switch`: finish the WebTyp rename + `form/input` → `input`

The framework migrated from `github.com/tinywasm/*` to the vanity path
`webtyp.com/*`, and **every framework module is now published** under the new
path. This module's imports were already rewritten to `webtyp.com/*` and its
`go.mod` requires were pinned to the current published tags (staged, uncommitted).
Two things remain.

## 1. The `webtyp.com/form/input` package no longer exists

`go build ./...` currently fails:

```
go: finding module for package webtyp.com/form/input
go: github.com/veltylabs/agent_switch imports
	webtyp.com/form/input: module webtyp.com/form@latest found (v0.4.7), but does not contain package webtyp.com/form/input
```

The input-widget package was **split out of `form` into its own module**:
`webtyp.com/form/input` → **`webtyp.com/input`**. The API is unchanged — same
constructor functions (`input.Checkbox()`, `input.Text()`, `input.Number()`, …)
returning the same `input.Input` type. The framework's own `webtyp.com/form`
already imports it this way (`form/render_input.go`: `import "webtyp.com/input"`).

### Do

1. In every `.go` file of this module, change the import
   `"webtyp.com/form/input"` → `"webtyp.com/input"`. Known call site:
   `model.go:5` (used at `model.go:59-61`: `input.Checkbox()`, `input.Text()`).
   Grep to be sure you caught all of them:
   ```
   grep -rn '"webtyp.com/form/input"' .
   ```
2. `go mod edit -require=webtyp.com/input@<current tag>` (find it:
   `go list -m -versions webtyp.com/input` → highest), then `go get
   webtyp.com/input` if needed. Remove `webtyp.com/form` from `go.mod` if
   nothing else in the module imports it (`grep -rn '"webtyp.com/form"' .`).
3. `go mod tidy`.

## 2. Verify the rename is complete

```
grep -rn 'github.com/tinywasm' --include='*.go' --include='go.mod' .   # must be empty
```
If anything remains, replace `github.com/tinywasm/` → `webtyp.com/` in `.go`
and `go.mod`, `github.com/tinywasm/` → `github.com/webtyp/` in `.md`/`.yml`.

## Acceptance

- `grep -rn 'webtyp.com/form/input' .` → empty.
- `grep -rn 'github.com/tinywasm' --include='*.go' --include='go.mod' .` → empty.
- `go build ./...` → clean.
- `gotest ./...` → all green (vet, race, tests).
- `go.mod` has no `replace … => ../…` line pointing outside the module.

## Constraints

- **No hardcoded strings** — if a widget-type or op name is repeated, it is a
  named constant in this package, not a literal.
- Do not change any behaviour: this is a package-path move plus the rename, not
  a redesign. The `input.*` constructor calls and their arguments stay
  byte-for-byte identical apart from the import path.
- This module is backend + WASM shared: keep the existing `//go:build` tags as
  they are.
