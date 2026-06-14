# PLAN — CKAD Trainer

Design and roadmap for a local, self-contained CKAD practice app that runs
scenarios against *your own* Kubernetes cluster: it sets up state, hands you a
task, checks your work, and cleans up after itself.

This is the working design doc. The README is the user-facing front door.

---

## 0. How to execute this plan (implementer: READ THIS FIRST)

This plan is built to be implemented step by step. Follow these rules literally.

**Work one phase at a time.** Phases are in §10. Do the lowest-numbered phase
that isn't done. Finish its **Definition of Done** completely, then STOP and
report to the user before starting the next phase. Never jump ahead.

**After every change, this must all pass before you continue:**
```bash
gofmt -l .        # prints nothing = formatted (run `gofmt -w .` to fix)
go vet ./...      # no warnings
go build ./...    # compiles
go test ./...     # all green (once tests exist)
```
If any fails, fix it before doing anything else. Do not move on with a red build.

**Hard rules (violating these breaks the app or the user's cluster):**
1. **Never touch a non-target cluster.** Before ANY `kubectl` call that creates,
   applies, patches, or deletes, verify the current context equals
   `config.safety.require_context`. If it doesn't, ABORT with a clear error.
   Read-only `get` is also gated through the same client so context is always set
   explicitly with `--context`.
2. **Always pass `--context <cfg.cluster.context>` to kubectl.** Never rely on
   the ambient current-context for mutations.
3. **Label everything you create** with the three labels in §4c. Cleanup depends
   on it.
4. **Scenarios are DATA, never code.** Do not hardcode any scenario logic in Go.
   The engine is generic and driven entirely by the YAML in `scenarios/`. If you
   feel the urge to special-case a scenario in Go, the schema is missing
   something — stop and ask the user.
5. **Do not invent kubectl flags or API fields.** Use the exact forms in §9c. If
   unsure, run `kubectl <cmd> --help` or `kubectl explain <path>` and match it.
6. **Use only the dependencies listed in §4b.** Adding any other module requires
   asking the user first.
7. **Follow the concrete examples in this doc literally.** When something is
   ambiguous, copy the nearest worked example (§A reference scenario, §4b
   contracts, §4c artifacts). Do not redesign the schema, CLI, or file layout.
8. **Match the function/type signatures in §4b exactly.** Other packages depend
   on them; don't rename or re-shape them.

**Stop and ask the user before:** adding a dependency, changing the scenario
schema, changing the CLI surface (§7), changing config keys (§8), or deleting any
file you didn't create in the current phase.

**Definition of Done, generally:** a phase is done when its specific DoD bullets
in §10 are met AND the four commands above are green AND you've committed the
work with a message naming the phase (e.g. `Phase 1: skeleton + doctor`).

**Module path:** `github.com/<user>/ckad-trainer` — confirm the exact path with
the user at `go mod init` time; use it consistently in all imports.
**Go version:** 1.22 (set `go 1.22` in `go.mod`).

---

## 1. Goals

- **Practice mode** — many small, self-contained scenarios that each drill a
  specific API surface (a field, a relationship between objects, an imperative
  command). Sometimes create from scratch, sometimes modify an existing object.
  The app says **PASS** or **FAIL** with a per-assertion breakdown.
- **Exam mode** — timed sessions that mimic the real exam: a mixed set of tasks
  weighted across the official domains, scored at the end. Questions modeled on
  the style of tasks reported from the real exam.
- **Cluster lifecycle** — the app creates the objects a scenario needs, lets you
  work, checks the result, and **cleans up** so the cluster returns to a clean
  state. Default target is **minikube**, but the cluster layer is abstracted so
  any kubeconfig context works.
- **Simple config** — a single `config.yaml`. No database, no services.

## 2. Non-goals (for now)

- No web UI. CLI-first. (A TUI is a possible later phase.)
- Not a replacement for reading the k8s docs — it *drives you back* to them.
- Not provisioning clusters from scratch beyond a thin `minikube start` helper.
  You bring a working cluster; we manage objects inside it.

---

## 3. Official exam shape (research baseline)

2-hour, performance-based, ~15–20 tasks across multiple clusters (context
switching matters). 2026 domain weights:

| Domain                                              | Weight |
|-----------------------------------------------------|:------:|
| Application Environment, Configuration & Security   | 25%    |
| Application Design and Build                         | 20%    |
| Application Deployment                               | 20%    |
| Services and Networking                              | 20%    |
| Application Observability and Maintenance            | 15%    |

Recurring practical themes from exam write-ups: imperative commands +
`--dry-run=client -o yaml` for speed, `kubectl explain` to recall fields, and a
lot of "modify this existing object" tasks rather than greenfield YAML.

Scenarios are tagged with the official domain so exam-mode sessions can sample by
weight.

---

## 4. Architecture

CLI-driven, **Go** (single static binary, `cobra` CLI à la kubectl). kubectl is
the source of truth (mirrors the exam); the app shells out to `kubectl` and
parses `-o json`. Scenarios are **data** (YAML), not code, so the catalog grows
without touching the engine.

```
ckad-trainer/
  PLAN.md  README.md  config.example.yaml
  go.mod  go.sum
  cmd/ckad-trainer/main.go      # cobra root; wires subcommands
  internal/
    config/    config.go        # load config.yaml, resolve context + kubectl path
    cluster/   cluster.go        # provider abstraction; doctor checks; namespace mgmt
    scenario/  scenario.go       # load + schema-validate a scenario file (yaml.v3)
    engine/    engine.go         # setup -> (user works) -> check -> cleanup state machine
    verify/    verify.go         # assertion evaluation against live cluster JSON
    exam/      exam.go           # timed multi-task session + scoring
    kubectl/   kubectl.go        # thin os/exec wrapper (json, apply, delete, get)
  scenarios/
    practice/<domain>/<id>.yaml
    exam/<set>/<id>.yaml
  state/                          # gitignored: tracks active scenarios for cleanup
```

Scenario files are embedded into the binary with `go:embed` (so a single
`ckad-trainer` ships with its whole catalog), with an override path in config to
load scenarios from disk during authoring.

### Why Go (decided)
- Single static binary — `go build` and run; no runtime to install on the box.
- `cobra` gives a kubectl-flavored CLI; familiar muscle memory.
- JSONPath assertions reuse **`k8s.io/client-go/util/jsonpath`** — the *same*
  engine `kubectl get -o jsonpath=` uses, so scenario paths match what you type.
- `gopkg.in/yaml.v3` for scenarios; `encoding/json` for cluster state.
- We still *call kubectl* rather than the API client, so what you practice is
  what you type on exam day.

Key deps: `spf13/cobra`, `gopkg.in/yaml.v3`, `k8s.io/client-go/util/jsonpath`.

### Isolation & cleanup
- Each scenario runs in a **dedicated namespace** (`ckad-<id>-<rand>`), recorded
  in `state/`. Cleanup = delete the namespace → kills everything namespaced.
- Cluster-scoped objects (PV, StorageClass, PriorityClass, IngressClass,
  Namespaces a task creates, etc.) are tracked explicitly by name in the
  scenario's `cleanup:` block and deleted individually.
- `cleanup --all` reconciles against `state/` and removes anything labeled
  `app.kubernetes.io/managed-by=ckad-trainer` as a safety net.
- Every object the app creates carries that label, so a stray cleanup can always
  find orphans by label selector.

---

## 4b. Package contracts (implement these signatures exactly)

These are the load-bearing types and functions. Implement them with these names
and shapes so packages compose. Bodies are yours; signatures are fixed. (Error
returns omitted for brevity where obvious — every fallible func returns `error`.)

```go
// ---- internal/config ----
type Config struct {
    Cluster struct {
        Provider  string `yaml:"provider"`   // "minikube" | "kubeconfig"
        Context   string `yaml:"context"`    // REQUIRED, non-empty
        Kubectl   string `yaml:"kubectl"`     // default "kubectl"
        AutoStart bool   `yaml:"auto_start"`
    } `yaml:"cluster"`
    NamespacePrefix string `yaml:"namespace_prefix"` // default "ckad"
    Defaults        struct {
        Exam struct {
            Count   int `yaml:"count"`
            Minutes int `yaml:"minutes"`
        } `yaml:"exam"`
    } `yaml:"defaults"`
    Safety struct {
        RequireContext string `yaml:"require_context"` // REQUIRED; gate for mutations
    } `yaml:"safety"`
    ScenarioDir string `yaml:"scenario_dir"` // "" => use embedded catalog
}
func Load(path string) (*Config, error) // reads file, applies defaults, validates

// ---- internal/kubectl ----
type Client struct { Bin, Context string }
func New(bin, context string) *Client
func (c *Client) run(args ...string) (stdout []byte, err error) // always injects --context
func (c *Client) CurrentContext() (string, error)               // `kubectl config current-context`
func (c *Client) GetJSON(kind, name, ns string) (map[string]any, error) // get -o json; ns "" = cluster-scoped
func (c *Client) ListJSON(kind, ns, selector string) ([]map[string]any, error)
func (c *Client) Apply(manifest string) error                   // apply -f - via stdin
func (c *Client) CreateNamespace(ns string, labels map[string]string) error
func (c *Client) DeleteNamespace(ns string) error               // --ignore-not-found --wait=false
func (c *Client) Delete(kind, name, ns string) error            // --ignore-not-found
func (c *Client) Raw(args ...string) ([]byte, error)            // for setup.commands / drills

// ---- internal/scenario ----
type Assert struct {
    Path     string `yaml:"path"`
    Equals   string `yaml:"equals,omitempty"`
    Contains string `yaml:"contains,omitempty"`
    In       []string `yaml:"in,omitempty"`
    Exists   *bool  `yaml:"exists,omitempty"`
    Matches  string `yaml:"matches,omitempty"`
    Gte, Lte string `yaml:"gte,omitempty"` // quantity-aware
    Wait     string `yaml:"wait,omitempty"` // e.g. "30s"; poll until pass or timeout
}
type Check struct {
    Kind         string   `yaml:"kind"`
    Name         string   `yaml:"name"`
    ClusterScoped bool    `yaml:"cluster_scoped,omitempty"`
    Assert       []Assert `yaml:"assert,omitempty"`
    Script       string   `yaml:"script,omitempty"`        // escape hatch
    CommandOutput *CmdOut `yaml:"command_output,omitempty"` // §9c drill mode
}
type Variant struct {
    Name   string  `yaml:"name"`
    Weight int     `yaml:"weight"` // default 1
    Prompt string  `yaml:"prompt"`
    Verify []Check `yaml:"verify"`
    Params map[string]Param `yaml:"params,omitempty"` // variant-local params
}
type Scenario struct {
    ID, Title, Mode, Domain string
    Weight          int
    EstimatedMinutes int    `yaml:"estimated_minutes"`
    Randomize       bool
    References      []string
    Params          map[string]Param
    Variants        []Variant            // empty => single implicit variant
    Setup           Setup
    Prompt          string               // used when Variants empty
    Hints           []string
    Verify          []Check              // used when Variants empty
    Solution        Solution
    Cleanup         Cleanup
}
func LoadAll(fsys fs.FS) ([]Scenario, error) // walk *.yaml, unmarshal, Validate each
func (s Scenario) Validate() error           // see §4d validation rules

// ---- internal/engine ----
type ObjRef struct { Kind, Name string }
type Instance struct {                 // the resolved draw; persisted to state/<id>.json
    ScenarioID    string            `json:"scenario_id"`
    RunID         string            `json:"run_id"`     // short random, goes in labels
    Seed          int64             `json:"seed"`
    Variant       string            `json:"variant"`    // chosen variant name, "" if none
    Namespace     string            `json:"namespace"`
    Params        map[string]string `json:"params"`     // fully-resolved values
    ClusterScoped []ObjRef          `json:"cluster_scoped"`
    StartedAt     time.Time         `json:"started_at"`
}
func Start(cfg *config.Config, s scenario.Scenario, seed int64) (*Instance, error)
func Check(cfg *config.Config, s scenario.Scenario, inst *Instance) (Report, error)
func Solution(s scenario.Scenario, inst *Instance) (string, error) // rendered text
func Cleanup(cfg *config.Config, inst *Instance) error

// ---- internal/verify ----
type Result struct { Path, Want, Got, Msg string; Pass bool }
type Report struct { Results []Result }
func (r Report) Passed() bool
func Evaluate(obj map[string]any, asserts []scenario.Assert) []Result // one object's checks
```

> If implementing a phase reveals a needed signature not listed here, add it in
> the same style and note it — do not change an existing one.

## 4c. Concrete artifacts (use these exact values)

**The three labels on every created object** (and on scenario namespaces):
```yaml
app.kubernetes.io/managed-by: ckad-trainer
ckad-trainer/scenario: <scenario.ID>
ckad-trainer/run: <Instance.RunID>
```
Engine injects these into setup manifests before apply (and the namespace gets
them at create). The user's own objects won't have them — that's fine; we verify
the user's objects by name, and only *delete* by namespace + tracked refs.

**Namespace naming** (`engine.makeNamespace`): `"<prefix>-<id>-<runid>"`,
lowercased, non-`[a-z0-9-]` replaced with `-`, truncated to 63 chars, must match
DNS-1123 (`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`). `RunID` = 5 lowercase-alnum chars.

**State file**: one JSON file per active scenario at `state/<scenario.ID>.json`
containing the `Instance` above. `start` writes it; `check`/`solution`/`cleanup`
read it; `cleanup` deletes it. If a `start` is run while a state file exists,
refuse unless `--force` (which cleans up first). `state/` is gitignored.

**`config.example.yaml`** (ship this verbatim):
```yaml
cluster:
  provider: minikube
  context: minikube
  kubectl: kubectl
  auto_start: true
namespace_prefix: ckad
defaults:
  exam:
    count: 16
    minutes: 120
safety:
  require_context: minikube
# scenario_dir: ./scenarios   # uncomment to author against disk instead of embed
```

**go.mod** (exact deps — pin to latest patch at init, ask before adding more):
```
module github.com/<user>/ckad-trainer
go 1.22
require (
    github.com/spf13/cobra v1.8.x
    gopkg.in/yaml.v3 v3.0.x
    k8s.io/client-go v0.30.x   // only for util/jsonpath
)
```

## 4d. Scenario validation rules (enforce in `Scenario.Validate`)

Reject (return error naming the file + field) if any hold:
- `ID` empty, or not unique across the catalog, or not DNS-1123 safe.
- `Mode` not in {`practice`, `exam`, `flashcard`}.
- `Domain` not one of the known domain slugs. We use **fine-grained** slugs (nicer
  for `list --domain`), defined as a const set in `internal/scenario`:
  `core-concepts`, `multi-container`, `pod-design`, `configuration`, `security`,
  `observability`, `services-networking`, `state-persistence`. Exam mode (Phase 5)
  maps each to an official 2026 domain + weight for sampling.
- Both `Variants` non-empty AND top-level `Verify`/`Prompt` non-empty (pick one
  form: either variants, or a single top-level prompt+verify).
- A `Check` has neither `Assert` nor `Script` nor `CommandOutput`.
- An `Assert` has `Path` empty, or zero matchers set, or >1 matcher set.
- `randomize: true` but no `Params` and no multi-`Variants` (nothing to randomize).
- Any `{{ ... }}` template fails to parse, or references a param not declared in
  `Params` (top-level or that variant's). Validate by rendering with **3 fixed
  seeds** and template-parsing every rendered field.
- `cluster_scoped: true` check whose object isn't also listed in `cleanup` (would
  leak). Warn-or-error: error.

---

## 5. Scenario schema

A scenario is one YAML file. Declarative assertions cover ~90% of cases; an
optional `script` escape hatch handles anything exotic.

> The block below is **annotated/illustrative** — it uses `...` to keep comments
> short. For a **complete, copy-pasteable scenario with zero placeholders**, use
> **Appendix A**. When authoring, start from Appendix A, not from this block.

```yaml
id: pv-pvc-pod-static
title: "Static PV → PVC → Pod"
mode: practice                 # practice | exam
domain: state-persistence      # maps to official domain group
weight: 1                      # sampling weight in exam mode
estimated_minutes: 6
references:                    # docs you're allowed to open during the exam
  - https://kubernetes.io/docs/concepts/storage/persistent-volumes/

# Objects pre-created before the user starts (the "modify existing" case).
setup:
  manifests:
    - |
      apiVersion: v1
      kind: ConfigMap
      ...
  commands:                    # optional imperative setup
    - kubectl label ns {ns} tier=practice

# What the user is asked to do (rendered as markdown).
prompt: |
  In namespace `{ns}`:
  1. Create a PersistentVolume `data-pv` (1Gi, hostPath /mnt/data, RWO).
  2. Create a PVC `data-pvc` that binds to it.
  3. Create a Pod `app` mounting the PVC at /data.

hints:
  - "kubectl explain pv.spec.capacity"
  - "PV is cluster-scoped — no namespace."

# Verification: list of checks. All must pass for PASS.
verify:
  - kind: PersistentVolume
    name: data-pv
    cluster_scoped: true
    assert:
      - path: .spec.capacity.storage     # jsonpath-ish dotted path
        equals: 1Gi
      - path: .spec.accessModes[*]
        contains: ReadWriteOnce
  - kind: PersistentVolumeClaim
    name: data-pvc
    assert:
      - path: .status.phase
        equals: Bound
  - kind: Pod
    name: app
    assert:
      - path: .spec.volumes[?(@.persistentVolumeClaim.claimName=="data-pvc")]
        exists: true
      - path: .status.phase
        in: [Running, Succeeded]

# Reference answer, shown on request or after PASS/FAIL.
solution:
  description: |
    Explanation of the key fields and why.
  manifests: [ ... ]
  commands: [ ... ]

# Cleanup. Namespace is auto-deleted; list cluster-scoped extras here.
cleanup:
  cluster_scoped:
    - kind: PersistentVolume
      name: data-pv
```

### Assertion vocabulary (`verify[].assert[]`)
`equals`, `not_equals`, `contains`, `not_contains`, `in`, `exists`,
`matches` (regex), `gte`/`lte` (numeric, with quantity parsing for `1Gi` etc.).
`equals`/`not_equals` are **quantity-aware**: when both the actual and expected
values parse as Kubernetes quantities they compare semantically, so `500m`==`0.5`
and `1Gi`==`1024Mi` match regardless of how they were written; non-quantity
fields keep strict string equality.
`path` is a dotted/bracketed JSONPath evaluated against `kubectl get -o json`.
A `wait:` option polls with timeout for status fields that take time to settle
(e.g. `Bound`, `Running`).

### Custom checks (escape hatch)
```yaml
verify:
  - script: checks/rollout_paused.sh   # exit 0 = pass; stdout = message
```
Runs with `{ns}` and `KUBECONFIG` in env. Used sparingly. (Scripts live on disk
alongside scenarios, not embedded.)

---

## 5b. Randomized / parameterized scenarios

A scenario can be **hard-coded** (fixed values, as in §5) or **randomized** so
you practice the *API* rather than memorizing one answer. Two independent axes:

- **Value randomness** — same task shape, different values each run. `replicas`
  2 vs 5, `port` 80 vs 8080, `storage` 1Gi vs 500Mi, `runAsUser` 1000 vs 2000,
  probe `path` `/` vs `/healthz`. You must *read* the task; rote answers fail.
- **Variant randomness** — different field/approach for the same concept. A
  ConfigMap consumed via `envFrom` **or** a single-key `configMapKeyRef` **or** a
  mounted volume. One variant is drawn per run; over many runs you cover the
  whole API surface for that object.

### How it works
The whole scenario file is a **Go `text/template`**. A `params` block declares
typed variables; the engine draws values, then renders the prompt, `setup`,
`verify` (paths *and* expected values), `solution`, and `cleanup` from that one
draw. Because assertions are rendered from the same values, the check is always
correct for whatever was randomized.

```yaml
id: configmap-consume
mode: practice
domain: configuration
randomize: true

# Value randomness: typed params, drawn at `start`.
params:
  cmName:  { pick: [app-config, web-config, db-config] }
  key:     { pick: [DATABASE_URL, LOG_LEVEL, FEATURE_FLAG, API_PORT] }
  value:   { pick: ["postgres://db:5432", "debug", "true", "8080"] }
  podName: { pattern: "consumer-{{randInt 100 999}}" }

# Variant randomness: engine draws one (weighted). Each is self-contained and
# may add its own params. Top-level params are in scope inside every variant.
variants:
  - name: envFrom-whole
    weight: 2
    prompt: |
      In `{{.ns}}`, ConfigMap `{{.cmName}}` already exists. Create Pod
      `{{.podName}}` (image busybox, cmd `sleep 3600`) that loads **all** keys
      of `{{.cmName}}` as environment variables.
    verify:
      - kind: Pod
        name: "{{.podName}}"
        assert:
          - path: .spec.containers[0].envFrom[?(@.configMapRef.name=="{{.cmName}}")]
            exists: true

  - name: single-key
    weight: 1
    prompt: |
      In `{{.ns}}`, expose only key `{{.key}}` of ConfigMap `{{.cmName}}` as env
      var `{{.key}}` in a new Pod `{{.podName}}` (image busybox, `sleep 3600`).
    verify:
      - kind: Pod
        name: "{{.podName}}"
        assert:
          - path: .spec.containers[0].env[?(@.name=="{{.key}}")].valueFrom.configMapKeyRef.name
            equals: "{{.cmName}}"
          - path: .spec.containers[0].env[?(@.name=="{{.key}}")].valueFrom.configMapKeyRef.key
            equals: "{{.key}}"

  - name: volume-mount
    weight: 1
    prompt: |
      In `{{.ns}}`, mount ConfigMap `{{.cmName}}` as a volume at `/etc/config`
      in a new Pod `{{.podName}}` (image busybox, `sleep 3600`).
    verify:
      - kind: Pod
        name: "{{.podName}}"
        assert:
          - path: .spec.volumes[?(@.configMap.name=="{{.cmName}}")]
            exists: true
          - path: .spec.containers[0].volumeMounts[?(@.mountPath=="/etc/config")]
            exists: true

# `setup` / `solution` are shared and also templated; they can branch on the
# chosen variant via {{if eq .variant "volume-mount"}} ... {{end}}.
setup:
  manifests:
    - |
      apiVersion: v1
      kind: ConfigMap
      metadata: { name: "{{.cmName}}", namespace: "{{.ns}}" }
      data: { "{{.key}}": "{{.value}}" }
```

### Param types
- `pick: [a, b, c]` — choose one from an explicit set (drives variants of value).
- `range: {min, max, step}` — integer (replicas, ports, delays, ids).
- `choice: [...]` with optional `weight` — like `pick` but weightable.
- `pattern: "{{...}}"` — string templated from helper funcs.
- Template helpers: `randInt`, `randPick`, `randName`, quantity helpers.
  Anything not drawn is deterministic given the seed.

### Reproducibility & consistency (the important part)
- At `start`, the engine draws a **seed** (or `--seed N`), resolves variant +
  params, renders the *concrete* instance, and writes it to `state/<id>.json`.
- `check`, `solution`, `cleanup`, and `reset` all read that resolved instance —
  never re-draw — so they always match what you were actually asked.
- `--seed` makes any run replayable (useful for sharing a tricky draw or for
  the smoke tests). Listing shows whether a scenario is `randomized`.
- Authoring guard (CI): for each randomized scenario, the schema test renders N
  seeds and validates every draw (template parses, all referenced params exist,
  verify paths are well-formed). The solution-applies-to-PASS smoke test runs a
  few seeds, not just one.

---

## 6. Engine lifecycle

```
start  ->  draw seed, pick variant + resolve params, render concrete instance,
           write state/<id>.json; create namespace, apply setup.manifests,
           run setup.commands, print prompt + hints
           (user now works in the cluster)
check  ->  load resolved instance; for each verify item: kubectl get -o json,
           evaluate assertions (with optional wait/poll); per-assertion PASS/FAIL
solution-> load resolved instance; render solution.description + manifests/commands
cleanup -> delete namespace, delete cluster_scoped objects, drop state/ entry
```

For randomized scenarios, the **resolved instance in `state/` is authoritative**:
`check`/`solution`/`cleanup` never re-draw, so they always match the prompt you
were given.

`check` is idempotent and re-runnable — iterate until green. `reset` =
cleanup + start (fresh attempt).

---

## 7. CLI surface

```
ckad-trainer doctor                 # kubectl present? context reachable? minikube up?
ckad-trainer list [--mode] [--domain] [--tag]
ckad-trainer start  <id> [--seed N] # setup + show task (--seed replays a draw)
ckad-trainer check  <id>            # verify, PASS/FAIL breakdown
ckad-trainer solution <id>
ckad-trainer cleanup <id> | --all
ckad-trainer reset  <id>
ckad-trainer random [--domain]      # pick a random practice scenario
ckad-trainer drill [--topic]        # flashcard runner for command-format drills (§9c)
ckad-trainer exam start [--count N] [--minutes M] [--domains ...]
ckad-trainer exam status            # time left, tasks done
ckad-trainer exam grade             # check all, weighted score, per-domain breakdown
```

Exam mode: samples N tasks weighted by domain %, sets a timer, lets you switch
between tasks, then grades everything and reports a score + which domains you're
weak in.

---

## 8. Config file (`config.yaml`)

Keep it minimal.

```yaml
cluster:
  provider: minikube           # minikube | kubeconfig
  context: minikube            # kube context to use (must be explicit, safety)
  kubectl: kubectl             # path/override
  auto_start: true             # `minikube start` if down (minikube provider only)
namespace_prefix: ckad
defaults:
  exam:
    count: 16
    minutes: 120
safety:
  require_context: minikube    # refuse to run if current context != this
```

`require_context` is a guardrail so we never touch a real cluster by accident.

---

## 9. Scenario catalog (build target)

Self-contained scenarios grouped by official domain. Markers:
**✅** = first batch to author · **🎲** = randomized (value and/or variant) ·
plain = hard-coded. Each is a single small task that isolates specific fields.

### App Environment, Configuration & Security (25%)
- ✅ 🎲 ConfigMap consume — **variants**: `envFrom` whole / single `configMapKeyRef`
  / volume mount; **params**: cm name, key, value (this is the §5b worked example)
- ✅ 🎲 Secret consume — **variants**: `secretKeyRef` env / `envFrom` / volume;
  **params**: created via `stringData` vs base64 `data`, key/value
- ✅ ConfigMap mounted as volume with `items` subpaths (hard-coded; field drill)
- 🎲 SecurityContext — **params**: `runAsUser`, `runAsNonRoot`, `fsGroup`,
  `readOnlyRootFilesystem`, `allowPrivilegeEscalation`; **variant**: pod-level vs
  container-level; **variant**: `capabilities.add` vs `drop`
- ServiceAccount: create + attach to pod; `automountServiceAccountToken: false`
- 🎲 ResourceQuota — **params**: which resource (cpu/memory/pods/secrets) + limit;
  task: create a pod that satisfies it
- LimitRange defaults; pod inherits request/limit
- ✅ 🎲 Resource `requests`/`limits` — **params**: cpu/memory quantities (must read)
- RBAC: Role + RoleBinding so an SA can `get pods` (verify with `auth can-i`)

### Application Design and Build (20%)
- ✅ 🎲 Multi-container pod sharing an `emptyDir` — **params**: mount paths, image;
  **variant**: sidecar-writes / sidecar-reads(log tail)
- ✅ initContainer that writes to emptyDir consumed by main container
- Sidecar (native sidecar = initContainer with `restartPolicy: Always`)
- 🎲 Job — **params**: `completions`, `parallelism`, `backoffLimit`,
  `activeDeadlineSeconds` (drawn so the numbers differ each run)
- 🎲 CronJob — **params**: `schedule`, `concurrencyPolicy`, history limits,
  `startingDeadlineSeconds`
- Pod `restartPolicy` + `command`/`args` override
- 🎲 downwardAPI — **variant**: env (`fieldRef`) vs volume; **params**: which field
  (pod name / namespace / labels / `resourceFieldRef`)
- Container image task: build/tag a Dockerfile, multi-stage (if docker available)

### Application Deployment (20%)
- ✅ 🎲 Deployment create + scale — **params**: name, image, replicas (draw), labels
- 🎲 Rollout: update image then `rollout status`/`history`/`undo` — **params**:
  from-image → to-image pair
- 🎲 Deployment strategy — **variant**: `RollingUpdate` (params `maxSurge`/
  `maxUnavailable`) vs `Recreate`
- Pause/resume a rollout
- Blue/green or canary via labels + two deployments + service selector swap
- Helm: install a chart, override a value, upgrade, rollback (if helm available)

### Services and Networking (20%)
- ✅ 🎲 Expose a Deployment — **variant**: ClusterIP / NodePort; **params**: `port`,
  `targetPort`, `nodePort`; verify endpoints populated
- Service targeting pods by label; fix a broken selector (debug task, hard-coded)
- ✅ 🎲 NetworkPolicy ingress — **params**: allowed-from label, port; **variant**:
  default-deny+allow-one vs namespace-selector
- NetworkPolicy egress restriction
- Ingress: host/path routing to a service (requires ingress addon)

### Observability and Maintenance (15%)
- ✅ 🎲 livenessProbe — **variant**: `httpGet` / `tcpSocket` / `exec`; **params**:
  `path`/`port`, `initialDelaySeconds`, `periodSeconds`, `failureThreshold`
- ✅ 🎲 readiness + startup probe — **params** as above; verify endpoint gating
- Debug a CrashLoopBackOff (read logs, `--previous`, fix command) — hard-coded
- `kubectl logs`, `describe`, events triage scenario — hard-coded
- Resource usage via `kubectl top` (metrics-server addon)
- 🎲 Fix a broken probe — **params**: which field is wrong (port/path/threshold)

> Rule of thumb: **debug/fix** scenarios are usually hard-coded (a specific bug to
> find); **author-from-spec** scenarios are usually randomized (drill the fields).

### Exam-mode sets
Curated mixed sets (default 16 tasks, weighted by domain) drawn from the above
plus harder multi-step composites. Composites are themselves randomized where it
helps. Archetypes modeled on reported exam tasks (original wording — see §9b).

---

## 9b. Exam-question archetypes (research-derived)

Modeled on the *shape* of tasks reported from real exams and the canonical
practice repos — original wording, randomizable params in `{{ }}`. Not leaked
verbatim questions.

1. **Make a Deployment reliable** — "Deployment `{{name}}` in ns `{{ns}}` must be
   more reliable: add a liveness-probe (`httpGet` `{{path}}` port `{{port}}`,
   `initialDelaySeconds {{d}}`, `periodSeconds {{p}}`)." *(modify existing)*
2. **Readiness gating** — "Add a readiness probe to `{{name}}` checking `{{path}}`
   on `{{port}}`; confirm pods only join Endpoints once ready."
3. **Expose + restrict** *(composite)* — "Create Deployment `{{name}}` (`{{n}}`
   replicas), expose it on `{{port}}`→`{{targetPort}}`, then add a NetworkPolicy
   so only pods labeled `{{k}}={{v}}` can reach it."
4. **CronJob with guards** — "Create CronJob `{{name}}` on `{{schedule}}` running
   `{{image}}`; set `backoffLimit {{b}}`, `activeDeadlineSeconds {{a}}`, keep only
   `{{h}}` successful histories."
5. **Job throughput** — "Run `{{image}}` as a Job: `completions {{c}}`,
   `parallelism {{par}}`, fail after `{{b}}` retries."
6. **Sidecar log shipper** *(multi-container)* — "Add a sidecar to `{{name}}` that
   tails `{{file}}` from a shared `emptyDir` the main container writes to."
7. **Config injection** — randomized ConfigMap/Secret consume (the §5b scenario)
   reused at exam difficulty with a distractor key.
8. **SecurityContext hardening** — "Run `{{name}}` as user `{{uid}}`, non-root,
   read-only root FS, drop capability `{{cap}}`."
9. **Rollout & undo** — "Update `{{name}}` image to `{{img}}`, verify the rollout,
   then roll back to the previous revision."
10. **Storage chain** — "PV `{{pv}}` (`{{size}}`, `{{mode}}`) → PVC `{{pvc}}` →
    Pod mounting it at `{{mountPath}}`." *(the §5 example, randomized)*
11. **Fix the broken thing** *(debug, hard-coded)* — a Service with a wrong
    selector / a probe with a wrong port / a CrashLoop from a bad `command`.
12. **Resource limits** — "Set `requests` `{{cpu}}/{{mem}}` and `limits`
    `{{cpu2}}/{{mem2}}` on container `{{c}}` of `{{name}}`."

These become the `scenarios/exam/<set>/` files in Phase 5; each maps to one or
more practice scenarios so weak exam areas point back to focused drills.

> **Sourcing policy:** all wording is original and paraphrased from publicly
> documented task *shapes* (k8s docs, open practice repos, candidate write-ups).
> We do not copy from paid simulators or "dumps." See §12.4.

---

## 9c. Imperative & command-format drills

A whole class of exam pain is knowing the *exact* imperative command + flag
format under time pressure. These get their own scenario flavor. Two delivery
modes:

- **Object-verified** — the task pushes you toward a command, but we check the
  *resulting object* (so any correct method passes). Most below.
- **`command-drill`** — the deliverable *is* the command/output (e.g. a jsonpath
  query). Verified by comparing normalized stdout, or flashcard-style (you type,
  we reveal + diff). New `verify` mode, see below.

### Imperative create — exact flags (object-verified, randomizable)
| Goal | Command shape | What we verify |
|------|---------------|----------------|
| ConfigMap from literals | `kubectl create cm {{n}} --from-literal=k1=v1 --from-literal=k2=v2` | `.data` keys/values |
| ConfigMap from env file | `kubectl create cm {{n}} --from-env-file=env.txt` | each line → a `.data` key (distinct from `--from-file`!) |
| ConfigMap from file w/ key | `kubectl create cm {{n}} --from-file=app.conf=./f` | key renamed to `app.conf` |
| Secret generic | `kubectl create secret generic {{n}} --from-literal=user={{u}}` | type `Opaque`, base64 `.data` |
| Secret docker-registry | `kubectl create secret docker-registry {{n}} --docker-server= --docker-username= --docker-password=` | type `kubernetes.io/dockerconfigjson`, `.dockerconfigjson` key |
| Secret TLS | `kubectl create secret tls {{n}} --cert=c.pem --key=k.pem` | type `kubernetes.io/tls`, `tls.crt`/`tls.key` |
| Job | `kubectl create job {{n}} --image={{img}} -- <cmd>` | job + template |
| **Job from CronJob** | `kubectl create job {{n}} --from=cronjob/{{cj}}` | template copied from the cronjob |
| CronJob | `kubectl create cronjob {{n}} --image={{img}} --schedule="{{sched}}" -- <cmd>` | `.spec.schedule` |
| Role | `kubectl create role {{n}} --verb=get,list --resource=pods --resource-name={{r}}` | rules |
| RoleBinding to SA | `kubectl create rolebinding {{n}} --role={{role}} --serviceaccount={{ns}}:{{sa}}` | `ns:sa` format! |
| RB to ClusterRole | `kubectl create rolebinding {{n}} --clusterrole={{cr}} --serviceaccount={{ns}}:{{sa}}` | namespaced binding to a clusterrole |
| Deployment | `kubectl create deploy {{n}} --image={{img}} --replicas={{r}} --port={{p}}` | spec |

### Modify in place — `set` family (object-verified)
| Goal | Command | Gotcha |
|------|---------|--------|
| Change image | `kubectl set image deploy/{{n}} {{c}}={{img}}` | `'*=img'` wildcard targets all containers |
| Env var | `kubectl set env deploy/{{n}} {{K}}={{V}}` | `--from=configmap/{{cm}}` injects a whole CM; `--keys=` filters |
| Resources | `kubectl set resources deploy/{{n}} --limits=cpu={{c}},memory={{m}} --requests=...` | |
| ServiceAccount | `kubectl set serviceaccount deploy/{{n}} {{sa}}` | |
| Selector | `kubectl set selector svc {{n}} app={{v}}` | |

### Run / expose / scale (object-verified)
| Goal | Command | Gotcha |
|------|---------|--------|
| Pod, no restart | `kubectl run {{n}} --image={{img}} --restart=Never -- sleep 3600` | **`run` only makes Pods now**; `--restart` sets `restartPolicy` only — it does **not** make a Job/Deployment (common stale-doc trap) |
| Pod w/ command | `kubectl run {{n}} --image={{img}} --command -- /bin/sh -c "..."` | `--command` vs trailing args = `args` |
| Temp shell | `kubectl run tmp --image=busybox --rm -it --restart=Never -- sh` | |
| Expose | `kubectl expose deploy {{n}} --port={{p}} --target-port={{tp}} --type={{type}} --name={{svc}}` | needs a selector on the source |
| Scale | `kubectl scale deploy {{n}} --replicas={{r}}` | |
| Autoscale | `kubectl autoscale deploy {{n}} --min={{a}} --max={{b}} --cpu-percent={{c}}` | |

### Rollout / label / annotate (object-verified)
- `kubectl rollout status|history|undo|pause|resume deploy/{{n}}`
- `kubectl rollout undo deploy/{{n}} --to-revision={{rev}}`
- `kubectl rollout history deploy/{{n}} --revision={{rev}}`
- `kubectl label po {{n}} {{k}}={{v}} --overwrite` · remove: `kubectl label po {{n}} {{k}}-`
- `kubectl annotate po {{n}} {{k}}={{v}}` · remove with trailing `-`

### Output-format / query drills (`command-drill` mode)
These produce *text*, not cluster state — verified by normalized stdout compare.
- `kubectl get po -o jsonpath='{.items[*].metadata.name}'` (curly-brace + `[*]`)
- `kubectl get po --sort-by='.status.containerStatuses[0].restartCount'` (leading dot, quoted)
- `kubectl get po --sort-by=.metadata.creationTimestamp`
- `kubectl get po -o custom-columns=NAME:.metadata.name,STATUS:.status.phase`
- `kubectl get po --field-selector=status.phase=Running`
- `kubectl get po -l 'env in (dev,prod)'` (set-based selector)
- `kubectl wait --for=condition=Ready pod/{{n}} --timeout={{t}}s`
- `kubectl wait --for=jsonpath='{.status.phase}'=Running pod/{{n}}`
- `kubectl logs {{n}} -c {{c}} --previous --since={{d}}` / `--tail={{k}}`
- `kubectl cp {{ns}}/{{pod}}:{{path}} ./out -c {{c}}`

### Gotchas the trainer teaches explicitly
- `run --restart` no longer generates Jobs/Deployments (above).
- `--record` is **deprecated** — don't rely on it for `rollout history` CHANGE-CAUSE.
- `--dry-run=client` (not bare `--dry-run`, which is deprecated/ambiguous).
- Secret YAML `data` is base64; `stringData` is plaintext; `kubectl create secret`
  encodes for you.
- RoleBinding `--serviceaccount` wants `namespace:name`, not just the name.
- `--from-env-file` (one key per line) ≠ `--from-file` (whole file as one key).

### New verify mode for `command-drill`
```yaml
verify:
  - command_output:
      run: "kubectl get po -n {{.ns}} -o jsonpath='{.items[*].metadata.name}'"
      # how to compare the student's answer / the canonical command's stdout
      normalize: sort_whitespace      # trim, collapse spaces, optional sort
      equals_solution: true           # compare to solution command's stdout
```
For pure flashcards (no cluster needed), a scenario can set `mode: flashcard`:
show the prompt, accept the typed command, reveal the canonical answer, and let
the user self-grade — useful for the jsonpath/sort-by muscle memory.

---

## 10. Build phases

Do them in order. Each lists its scope and an explicit **Done when** checklist.
Do not start a phase until the previous one's checklist is fully green. The four
commands in §0 must pass at the end of every phase.

### Phase 1 — Skeleton + doctor
Scope: `go mod init`; cobra root in `cmd/ckad-trainer/main.go` with stub
subcommands (§7) that print "not implemented"; `internal/config` (`Load` +
defaults + validation); `internal/kubectl` (`Client`, `run` always injecting
`--context`, `CurrentContext`, `GetJSON`); `internal/cluster` doctor checks;
`doctor` command; a `scripts/minikube-up.sh` helper (install hint + `minikube
start`). Ship `config.example.yaml` (§4c) and `.gitignore` (ignore `state/`,
the built binary).
**Done when:**
- `go build ./...` produces a `ckad-trainer` binary.
- `ckad-trainer doctor` prints: kubectl found + version, current context, whether
  it equals `require_context` (and a clear FAIL line if not), cluster reachable
  (`kubectl version`/`get ns`), minikube status if provider=minikube.
- `doctor` exits non-zero when the context guard fails or the cluster is
  unreachable; zero when all green.
- `config.Load` rejects a config with empty `cluster.context` or
  `safety.require_context` (unit test).

### Phase 2 — Engine MVP (one hard-coded scenario)
Scope: `internal/scenario` (`LoadAll` from a disk dir for now, `Validate` per
§4d); `internal/engine` `Start`/`Cleanup` (no randomness yet); namespace
creation with the §4c labels; state file read/write (§4c); manifest label
injection; `start`/`cleanup` CLI wired. Author ONE scenario:
`scenarios/practice/state-persistence/pv-pvc-pod-static.yaml` (the §5 / §A
example, non-randomized).
**Done when:**
- `ckad-trainer start pv-pvc-pod-static` creates the namespace, applies setup,
  writes `state/pv-pvc-pod-static.json`, prints the prompt.
- `kubectl get ns -l app.kubernetes.io/managed-by=ckad-trainer` shows the ns with
  all three labels.
- `ckad-trainer cleanup pv-pvc-pod-static` deletes the namespace AND the
  cluster-scoped PV from `cleanup:`, and removes the state file.
- Running `start` twice without cleanup is refused unless `--force`.

### Phase 3 — Verify engine
Scope: `internal/verify` `Evaluate` over `client-go/util/jsonpath`; full matcher
set (§5 assertion vocabulary) incl. quantity-aware `gte/lte` and `wait` polling;
`engine.Check` + `Solution`; `check`/`solution` CLI with a per-assertion PASS/FAIL
table; `command_output` verify mode and `flashcard` mode (§9c).
**Done when:**
- `verify` unit tests pass against canned JSON fixtures for every matcher
  (`internal/verify/verify_test.go`), no cluster needed.
- `ckad-trainer check pv-pvc-pod-static` prints a row per assertion and an overall
  PASS/FAIL; a correct solution → PASS, a deliberately wrong object → FAIL.
- `ckad-trainer solution pv-pvc-pod-static` renders the reference answer.
- `wait` actually polls (test: a Pod that becomes Running within the timeout).

### Phase 4 — Practice catalog batch 1 (+ randomization)
Scope: implement randomization (§5b) — `Params` draw, weighted `Variants` pick,
seeded RNG, template render of prompt/setup/verify/solution, persisted resolved
`Instance`; `--seed`. `go:embed` the catalog; `scenario_dir` override. Author all
**✅** scenarios (§9) plus the object-verified command drills (§9c).
**Done when:**
- Every ✅ scenario passes the smoke test (its own `solution` applied → PASS;
  a wrong manifest → FAIL) across **3 seeds** each, run on minikube.
- The schema/validation test (§11) loads the whole embedded catalog with zero
  errors across 3 fixed seeds.
- `ckad-trainer start configmap-consume --seed 42` is reproducible (same variant
  + params on repeat); `check`/`solution` use the persisted draw.
- `list` shows mode + domain + a 🎲 marker for randomized scenarios.

### Phase 5 — Exam mode
Scope: `internal/exam` — weighted sampling by domain %, session state (timer,
per-task status) on disk, `exam start/status/grade`; grade = weighted score +
per-domain breakdown.
**Done when:**
- `ckad-trainer exam start --count 8 --minutes 60` sets up 8 tasks sampled across
  domains and starts a timer; `exam status` shows time left + tasks done.
- `exam grade` checks all tasks, prints a weighted score and per-domain breakdown,
  then cleans up the whole session.

### Phase 6 — Catalog fill-out
Scope: remaining (non-✅) scenarios per domain + output-format/flashcard drills
(jsonpath, `--sort-by`, custom-columns, `wait --for`). Same smoke + schema bars.

### Phase 7 — Polish
Scope: `random`, `drill` (flashcard runner), nicer/colorized output, optional
TUI, build/release packaging. No new schema or CLI changes without asking.

## 11. Testing strategy

- **Unit (no cluster):** `internal/verify` against canned JSON fixtures for every
  matcher; `internal/config` defaults/validation; `internal/scenario` template
  render with 3 fixed seeds.
- **Catalog schema test:** one test loads the entire embedded catalog and runs
  `Validate` on each across 3 fixed seeds. CI fails if any scenario is invalid.
- **Smoke (needs minikube):** for each scenario, apply its own `solution` →
  expect PASS; apply a deliberately-wrong variant → expect FAIL. Run randomized
  scenarios across 3 seeds. Gate this behind a build tag/env so unit tests stay
  cluster-free (`go test -tags=cluster ./...`).
- A change is not "done" until the relevant tier is green (§0).

## 12. Decisions

1. **Language**: **Go** — single static binary, cobra CLI, `client-go` jsonpath
   for assertions. *(decided)*
2. **Cluster target**: **minikube** default; Phase 1 adds an install/start helper
   (not yet present on this box). *(decided)*
3. **Checks**: kubectl `-o json` parsed in-process via `client-go/util/jsonpath`.
   *(decided)*
4. **Scope of "exam questions"**: model the *style/shape* of reported exam tasks
   (safe, original wording) rather than reproducing leaked questions verbatim.
   *(default — say if you want otherwise)*

---

## 13. Guardrails & common pitfalls (implementer)

**Cluster safety**
- Gate EVERY mutation on `currentContext == cfg.Safety.RequireContext`. Put the
  check in one place (`kubectl.Client` or `engine`) so it can't be bypassed.
- Always `--context` on kubectl. Never `kubectl delete ns` without
  `--ignore-not-found`. Never delete by label alone in normal flow — delete by
  the namespace + tracked `ClusterScoped` refs; label-delete is only the
  `cleanup --all` safety net.
- Cluster-scoped objects (PV, StorageClass, ClusterRole, etc.) are NOT in the
  scenario namespace — they must be in `cleanup.cluster_scoped` or they leak.

**Randomization**
- Draw once at `start`, persist to `state/`, and NEVER re-draw in
  `check`/`solution`/`cleanup`. A re-draw = the check won't match the prompt.
- Seed the RNG explicitly (`math/rand` with the stored seed). No `time.Now()`
  inside render paths — that breaks reproducibility.
- Render templates with `text/template` (NOT `html/template` — it would escape
  YAML/quotes). Use `missingkey=error` so an undeclared param fails loudly.

**kubectl correctness (don't repeat stale-doc errors)**
- `kubectl run` creates ONLY Pods now; `--restart` just sets `restartPolicy`.
  Don't claim it makes Jobs/Deployments.
- Use `--dry-run=client`, never bare `--dry-run`.
- Secret YAML: `data` = base64, `stringData` = plaintext. When you generate setup
  manifests with secrets, prefer `stringData` to avoid hand-encoding.
- RoleBinding `--serviceaccount=<ns>:<name>` (namespace-qualified).
- `--from-env-file` (one key per line) ≠ `--from-file` (whole file as one value).

**Verify / JSONPath**
- Feed `kubectl get -o json` output (a `map[string]any`) to
  `client-go/util/jsonpath`. Paths in scenarios use kubectl jsonpath syntax
  (e.g. `{.spec.containers[0].image}`); the leading `{}` may be added by the
  evaluator — be consistent and document which form scenario authors write.
- Filter expressions like `[?(@.name=="x")]` are supported by the client-go
  evaluator; test them in fixtures before relying on them in a scenario.
- Quantities (`1Gi`, `100m`) are strings in JSON — parse with a quantity helper
  for `gte/lte`, don't string-compare.
- Status fields (`.status.phase=Bound/Running`) take time — use `wait:`; don't
  assume immediate.

**Engine / state**
- Treat `state/<id>.json` as the source of truth post-`start`. If it's missing,
  `check`/`cleanup` should error with "run `start` first" — don't silently
  re-draw.
- Make `check` idempotent and side-effect-free (read-only). Only `start`/`cleanup`
  mutate.

**Scope discipline**
- Don't gold-plate. No TUI, colors, or extra commands before Phase 7.
- Don't add scenarios in a phase that doesn't call for them.

## 14. Glossary

- **Scenario** — one YAML task definition in `scenarios/` (data, not code).
- **Variant** — one alternative form of a scenario (e.g. `envFrom` vs volume);
  the engine draws one per run.
- **Param** — a declared variable drawn at `start` (value randomness).
- **Instance** — the resolved, persisted result of a draw (`state/<id>.json`):
  chosen variant + concrete param values + namespace + run id + seed.
- **Draw** — the act of choosing a variant and param values from a seed.
- **Check / Assert** — a verification unit; an `Assert` is one path+matcher.
- **Object-verified vs command-drill** — verify the resulting cluster object, vs
  verify the command/its text output (§9c).
- **Domain** — one of the five official 2026 exam domains (§3).

---

## Appendix A — Complete reference scenario (zero placeholders)

Copy this as the template for new scenarios. Every field is filled; nothing is
`...`. This is the Phase 2 scenario (non-randomized) — file path:
`scenarios/practice/state-persistence/pv-pvc-pod-static.yaml`.

```yaml
id: pv-pvc-pod-static
title: "Static PV → PVC → Pod"
mode: practice
domain: state-persistence
weight: 1
estimated_minutes: 6
randomize: false
references:
  - https://kubernetes.io/docs/concepts/storage/persistent-volumes/

setup:
  manifests: []          # nothing pre-created; this is a build-from-scratch task
  commands: []

prompt: |
  In namespace `{{.ns}}`:
  1. Create a PersistentVolume `data-pv`: 1Gi, accessMode ReadWriteOnce,
     hostPath `/mnt/data`.
  2. Create a PersistentVolumeClaim `data-pvc` (same ns) that binds to it
     (1Gi, ReadWriteOnce).
  3. Create a Pod `app` (image `nginx:1.27`) that mounts `data-pvc` at `/data`.

hints:
  - "PV is cluster-scoped — it has no namespace."
  - "kubectl explain pv.spec.capacity"
  - "The PVC must request <= the PV's capacity and a compatible accessMode."

verify:
  - kind: PersistentVolume
    name: data-pv
    cluster_scoped: true
    assert:
      - path: "{.spec.capacity.storage}"
        equals: "1Gi"
      - path: "{.spec.accessModes[*]}"
        contains: "ReadWriteOnce"
      - path: "{.spec.hostPath.path}"
        equals: "/mnt/data"
  - kind: PersistentVolumeClaim
    name: data-pvc
    assert:
      - path: "{.status.phase}"
        equals: "Bound"
        wait: "30s"
  - kind: Pod
    name: app
    assert:
      - path: '{.spec.volumes[?(@.persistentVolumeClaim.claimName=="data-pvc")].name}'
        exists: true
      - path: "{.status.phase}"
        in: ["Running", "Succeeded"]
        wait: "60s"

solution:
  description: |
    PV is cluster-scoped (no namespace). The PVC binds by matching capacity +
    accessMode. The Pod references the PVC by claimName under spec.volumes, then
    mounts that volume into the container.
  commands:
    - "# all-in-one apply"
  manifests:
    - |
      apiVersion: v1
      kind: PersistentVolume
      metadata:
        name: data-pv
      spec:
        capacity:
          storage: 1Gi
        accessModes: ["ReadWriteOnce"]
        hostPath:
          path: /mnt/data
    - |
      apiVersion: v1
      kind: PersistentVolumeClaim
      metadata:
        name: data-pvc
        namespace: "{{.ns}}"
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 1Gi
    - |
      apiVersion: v1
      kind: Pod
      metadata:
        name: app
        namespace: "{{.ns}}"
      spec:
        containers:
          - name: app
            image: nginx:1.27
            volumeMounts:
              - name: data
                mountPath: /data
        volumes:
          - name: data
            persistentVolumeClaim:
              claimName: data-pvc

cleanup:
  cluster_scoped:
    - kind: PersistentVolume
      name: data-pv
```

Notes for the implementer:
- `{{.ns}}` is the only template var in a non-randomized scenario; the engine
  always provides `.ns` (the scenario namespace) and `.variant`.
- The engine injects the §4c labels into each `setup`/`solution` manifest's
  `metadata.labels` before applying. (Here `setup` is empty; `solution` manifests
  are applied only by the user, or by the smoke test.)
- PV `data-pv` is cluster-scoped, so it's listed in `cleanup.cluster_scoped`.
