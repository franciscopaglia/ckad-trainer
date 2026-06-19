# ckad-trainer

A local CLI to practice for the CKAD exam: it sets up scenarios in *your*
cluster, checks your work (PASS/FAIL), and cleans up — plus a timed exam mode.
Scenarios are YAML **data**, not code; the engine is generic.

- **User guide:** `USAGE.md`. **Build/contribute:** `README.md`. Architecture,
  scenario schema, and invariants live in package doc comments and nested
  `CLAUDE.md` files (start with `internal/engine/CLAUDE.md`).

## Layout

- `cmd/ckad-trainer/` — cobra CLI.
- `internal/engine/` — scenario lifecycle, verification, randomization. **See
  `internal/engine/CLAUDE.md`.**
- `internal/scenario/` — YAML schema, loader, validator. `internal/verify/` —
  jsonpath assertion eval. `internal/exam/` — timed sessions + scoring.
- `internal/config/`, `internal/cluster/` (safety guard), `internal/kubectl/`
  (exec wrapper, always injects `--context`), `internal/paths/` (XDG config/state).
- `scenarios/practice/<domain>/*.yaml`, `scenarios/flashcards/*.yaml` —
  embedded via `catalog.go`.

## Commands

- `make check` — fmt + vet + cluster-free tests + license headers (pre-commit gate).
- `make smoke` — cluster smoke: starts every scenario ×3 seeds, applies its own
  solution, asserts the check passes. Mutates the cluster in `config.yaml`.
- `make build` / `make dist VERSION=vX.Y.Z` — binary / cross-compiled release + checksums.
- Adding a scenario: drop a YAML file under `scenarios/`; validate with
  `make test`, prove solvable with `make smoke`. Flashcards are smoke-skipped.

## Keep docs current (token optimization)

These docs are loaded into the model's context to avoid re-reading code, so
**stale docs cost tokens and mislead**. When you change behavior, update the
relevant doc in the same change:

- **Nested `CLAUDE.md`** (e.g. `internal/engine/CLAUDE.md`) — package
  invariants, state schema, control flow. Update when those change; add a new
  nested `CLAUDE.md` when a package grows non-obvious invariants worth not
  re-deriving.
- **Package/exported doc comments** — keep accurate; they are the cheapest docs.
- **`README.md` / `USAGE.md`** — build/contribute and user-facing behavior
  (e.g. commands, flags, the scenario count) respectively.

Prefer updating an existing doc over adding prose elsewhere; don't duplicate the
same fact across files.
