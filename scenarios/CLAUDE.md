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

## Catalog backlog (specs — implement top to bottom, delete entries when done)

When adding any of these, also bump the scenario counts in README.md (intro)
and USAGE.md (§10 intro + the domain table's "Covered" cell) in the same
change, and prove it with the subset smoke run above.

1. **`pod-affinity`** (core-concepts, randomize, two variants) — setup
   creates a labeled anchor Pod (e.g. `app=cache`). Variant `co-locate`:
   required podAffinity on that label, `topologyKey: kubernetes.io/hostname`,
   assert affinity fields + Running (wait). Variant `spread`: podAntiAffinity
   — MUST be `preferredDuringSchedulingIgnoredDuringExecution` (required
   anti-affinity can never schedule on a single-node cluster); assert the
   preferred term's fields + Running.
2. **`crd-instance`** (configuration) — setup applies a tiny Namespaced-scope
   CRD and waits `--for condition=Established`; task: discover it
   (`api-resources`/`explain`) and create a custom resource in the scenario
   namespace; verify fetches the CR by `<plural>.<group>` and asserts a spec
   field. **Gotchas:** the CRD is cluster-scoped → must be listed in
   `cleanup.cluster_scoped` (the validator only forces this for cluster-scoped
   *checks*, so remember it for setup-created CRDs); make the group unique per
   run (pattern param, e.g. `training{{randInt 100 999}}.example.com`) so
   back-to-back runs don't race the CRD's async deletion; CRD name must equal
   `<plural>.<group>`.

Rejected for now: Helm/Kustomize/docker hands-on (need tooling/files outside
the cluster — flashcards cover them), API-deprecation hands-on (can't set up
a deprecated-but-served API version portably).

> Keep this file in sync when assertion/render/cleanup semantics change.
