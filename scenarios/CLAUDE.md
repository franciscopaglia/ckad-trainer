# scenarios/ — authoring rules

Scenario YAML only; the engine is generic. Schema = the Go types in
`internal/scenario/scenario.go`; render/check/cleanup model in
`internal/engine/CLAUDE.md`. Validate with `make test` (loads + renders every
scenario across seeds); prove solvable with `make smoke`, or one scenario:

    go test -tags=cluster -run 'TestSmokeSolutions/<id>$' .

## Assertion style (bug classes fixed once already — don't reintroduce)

- **Assert exactly what the prompt demands.** Every requirement in the prompt
  needs an assert (else wrong answers pass — e.g. an unchecked volumeMount),
  and no assert may demand what the prompt didn't say (else right answers
  fail). When you edit a prompt, re-derive its asserts.
- **Never index into lists the user doesn't fully control.** Kubernetes
  appends the SA token volumeMount; `kubectl debug` appends to
  `ephemeralContainers` (entries can NEVER be removed, so a wrong first
  attempt must not block the check); container/env/envFrom order is the
  user's choice. Use `[*]` + `contains`, or a name filter
  (`[?(@.name=="x")]`), instead of `[0]`.
- **Matcher semantics** (`internal/verify`): `contains` is exact element
  membership of the selected list, NOT substring match. `equals` requires the
  path to select exactly ONE value and is quantity-aware (`"500m"` == `"0.5"`,
  `"1Gi"` == `"1024Mi"`). `exists: false` passes when the path selects
  nothing — use it to forbid a field (e.g. a canary Service must not select
  `track`).
- Prompts must not dictate incidental structure ("list `app` first" is a
  smell — filter by name instead).
- Assertions on settling state (Running/Bound/readyReplicas/Endpoints) need
  `wait:`; a missing object still fails immediately.

## Randomization

- `params` (pick/choice/range/pattern) + weighted `variants`; same seed ⇒ same
  draw. Every templated field — prompt, **hints**, setup, solution, cleanup —
  is rendered with `missingkey=error` across seeds by `make test`, so
  template typos fail loudly. Hints are shown rendered; use `{{.ns}}` etc.
  freely in them.

## Setup & cleanup

- The namespace delete is automatic; everything else must be declared.
  `cleanup.cluster_scoped` for objects (the validator enforces it for
  cluster-scoped checks; deletion is `--ignore-not-found`; give them
  per-run-unique names via a `pattern` param). `cleanup.commands` for state
  the object model can't express (e.g. node labels added in
  `setup.commands`) — kubectl-only, rendered at start, and **must be
  idempotent**: a failed cleanup is retried whole.
- Solution commands are kubectl-only (a leading `kubectl` is stripped, then
  run via the context-injecting wrapper); helm/docker/kustomize-CLI content
  belongs in flashcards (`mode: flashcard`, smoke-skipped, the shown answer
  is `solution.commands[0]`).

> Keep this file in sync when assertion/render/cleanup semantics change.
