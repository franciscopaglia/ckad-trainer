# internal/engine

Drives a scenario's lifecycle against the cluster. Generic: behavior comes from
scenario YAML (see `internal/scenario`), not from code here.

**Files:** `engine.go` (lifecycle + Instance state), `check.go` (verification +
solution apply), `random.go` (seeded draws). Tests: `state_test.go`,
`random_test.go`.

## Lifecycle

`Start` → user works → `Check` → `Cleanup`. `RecordCheck` persists the check
result so `status` can show progress. The task text shown to the user comes from
`RenderPrompt` *and* `RenderHints` — hints are templated fields too and must be
rendered with the instance's draw, never printed raw.

- **Start**: guards the safety context (`cluster.Guard`), refuses if the id
  already has state, draws variant+params from the seed, creates a labeled
  namespace, applies `setup` manifests/commands, records the cluster-scoped
  cleanup targets, writes state. **Any failure after the namespace exists rolls
  back** (`rollbackStart` → `teardown`): the state file isn't written yet, so
  without rollback the namespace and any setup side effects would be orphaned.
- **Check**: read-only on the cluster. Returns a `CheckReport`. Does **not**
  write result fields — the `check` command calls `RecordCheck` afterward.
- **Cleanup**: guards the safety context, then `teardown` (namespace + tracked
  cluster-scoped objects + cleanup commands) + removes the state file.
- **Scenario commands run without a shell**: setup/cleanup/solution commands
  are split quote-aware (`splitWords`/`kubectlArgs` in `words.go` — single/double
  quotes, backslash escapes; blanks and `#` comments skipped; leading `kubectl`
  stripped) and passed as argv to the context-injecting client. `CheckDraw`
  validates that every rendered command splits cleanly, so unbalanced quotes
  fail `make test`, not the cluster. No pipes/redirects/`$VARS` — it's argv,
  not a shell.

## Instance & state model

- State is one JSON file per scenario id: `<StateDir>/<id>.json`. `StateDir`
  comes from `internal/paths` (XDG; overridable via `CKAD_TRAINER_STATE`) —
  never a CWD-relative path.
- **One active instance per scenario id.** `statePath` is keyed by id alone;
  `Start` returns `ErrAlreadyStarted` if it exists. `RunID` (5 chars) only makes
  the *namespace* unique (`<prefix>-<id>-<runid>`); it is not an addressing key.
- `Instance.Passed` / `CheckedAt` hold the last check outcome (set only by
  `RecordCheck`; zero `CheckedAt` = not yet checked).
- `LoadActiveInstances` enumerates state files (skips `exam.json`), oldest first.

## Invariants & gotchas (don't break these)

- **`renderCheck` must not mutate the input check.** Scenarios are loaded once
  and reused across draws/seeds; the `Assert`/`In` slices share backing arrays,
  so `renderCheck` copies them into fresh slices. Mutating in place corrupts
  other draws.
- **Determinism.** `draw` resolves params in **sorted key order**
  (`sortedKeys`) so Go map iteration can't affect the result; a later param may
  reference an earlier one via the data map. Same seed ⇒ same variant+params.
  `seed == 0` means "pick one" (time-based); non-zero is reproducible.
- **`render` uses `missingkey=error`** — templates fail loudly on unknown keys.
- **Cluster-scoped objects** (ClusterRole, PriorityClass, …) must be listed in
  the scenario's `cleanup.cluster_scoped`; the validator enforces it. Their
  names are rendered at Start and stored in `Instance.ClusterScoped` for
  deletion. `kubectl Delete` uses `--ignore-not-found`, so listing an object a
  given variant didn't create is safe. Use a per-run-unique name (e.g. a
  `pattern` param) to avoid collisions between concurrent runs.
- **Cleanup commands** (`cleanup.commands`) undo cluster state the object model
  can't express (e.g. a node label added by a setup command). They are rendered
  at Start into `Instance.CleanupCommands`, run kubectl-only after the
  namespace/object deletion, and **must be idempotent** — a failed cleanup keeps
  the state file and is retried whole.
- **Labels** on everything created: `app.kubernetes.io/managed-by=ckad-trainer`,
  `ckad-trainer/scenario`, `ckad-trainer/run`. Namespace cleanup is by name;
  `status` reconciliation lists namespaces by the managed-by label.
- **Namespace names**: `makeNamespace` lowercases, maps non-DNS-1123 chars to
  `-`, truncates to 63.

## Verification (check.go)

Three check kinds, dispatched in `checkWith`:
- **object asserts** → `evalObject`: fetch the object, evaluate asserts
  (`internal/verify`, client-go jsonpath). Polls while any assert declares
  `wait`; stops when all pass, the object is absent, or the deadline passes.
- **script** → `evalScript`: runs `sh <script>` with `NS=<namespace>` in env;
  exit 0 = pass, stdout = message.
- **command_output** → `evalCommandOutput`: runs a shell command; with
  `equals_solution` compares normalized stdout to the first solution command's
  output, else passes if it ran.

`Check` (waits) is used by the `check` command and exam grade; `CheckQuick`
(snapshot, no wait) by exam status. `resolveChecks` returns the chosen variant's
checks, or top-level checks when there are no variants.

`ApplySolution` applies the solution manifests + commands to the cluster (used by
the cluster smoke test to prove solvability). Commands are kubectl-only: a
leading `kubectl` is stripped, `#`-comments and blanks skipped; non-kubectl
solution commands (helm/docker) will not work here — keep those as flashcards.

## Randomization (random.go)

Param types: `pick`, `choice`, `range` (min/max/step), `pattern`. Pattern funcs:
`randInt min max`, `randPick a b c`, `randName prefix`. Variants are weighted
(`weight`, default 1). `CheckDraw` renders every templated field for a seed
without touching the cluster — the catalog validation test runs it across seeds,
so a new scenario that renders cleanly there is structurally sound.

> Keep this file in sync when you change engine behavior, invariants, the state
> schema, or the check/draw model — it is loaded into context to save re-reading
> the package.
