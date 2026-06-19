# Using CKAD Trainer

A complete guide to practising with `ckad-trainer`. If you just want to build it
and start hacking on the code, see the [README](./README.md).

> **Disclaimer:** independent study aid, not affiliated with or endorsed by The
> Linux Foundation or CNCF. It contains **no real exam questions** — every
> scenario is original, based on the public [CKAD curriculum](https://github.com/cncf/curriculum)
> and community exam testimonials, and may not match the live exam in content or
> difficulty. See the README [Disclaimer](./README.md#disclaimer) for the full
> note and trademark attribution.

## Table of contents

- [1. Install and set up](#1-install-and-set-up)
- [2. The two modes](#2-the-two-modes)
- [3. Practice workflow](#3-practice-workflow)
- [4. Randomized scenarios and seeds](#4-randomized-scenarios-and-seeds)
- [5. Managing active scenarios](#5-managing-active-scenarios)
- [6. Flashcard drills](#6-flashcard-drills)
- [7. Exam mode](#7-exam-mode)
- [8. Command reference](#8-command-reference)
- [9. Configuration](#9-configuration)
- [10. Scenario catalog](#10-scenario-catalog)
- [11. Tips and troubleshooting](#11-tips-and-troubleshooting)

---

## 1. Install and set up

You need a Kubernetes cluster you don't mind scribbling on, `kubectl`, and the
`ckad-trainer` binary — download it from the
[latest release](https://github.com/franciscopaglia/ckad-trainer/releases/latest)
and put it on your `PATH` (see the [README](./README.md#install)), or build from
source.

```bash
ckad-trainer init       # writes config.yaml from your current kube context
ckad-trainer doctor     # verify kubectl, the context guard, reachability
```

You don't *have* to run `init`: with no `config.yaml`, ckad-trainer falls back to
whatever context `kubectl` is currently pointed at. Note that without a config
there's no pinned context, so the safety guard is inactive — the app follows your
current context each time, so a `kubectl` context switch mid-session can land
`cleanup`/`check` on a different cluster. `init` pins that choice (see the box
below). To target a different cluster: `ckad-trainer init --context <name>`, or
edit `config.yaml` (§9).

`doctor` must be green before anything else works. It checks that `kubectl` is
present, that your current context matches the one you allow-listed
(`safety.require_context`), and that the cluster answers.

```
[+] kubectl            v1.30.0
[+] current-context    minikube
[+] context guard      current == require_context ("minikube")
[+] config invariant   cluster.context == require_context ("minikube")
[+] cluster reachable  context "minikube" responds

doctor: ready
```

> **Safety:** the app only ever talks to the context named in `config.yaml`, and
> refuses to run unless that context is also your current one. It can't touch a
> real cluster by accident.

---

## 2. The two modes

**Practice mode** — many small, focused scenarios, each isolating one piece of
the API (a ConfigMap consumed via `envFrom`, a `securityContext`, a PV→PVC→Pod
chain, a default-deny NetworkPolicy, a broken probe to fix...). You work in the
cluster, then ask the app to grade it.

**Exam mode** — a timed session that samples tasks weighted by the real exam's
domain breakdown, then scores everything at the end with a per-domain "where
you're weak" report.

---

## 3. Practice workflow

The core loop is **start → work → check → cleanup**.

```bash
ckad-trainer list                         # browse the catalog (🎲 = randomized)
ckad-trainer start configmap-consume      # creates an isolated namespace + prints the task
```

`start` prints something like:

```
Consume a ConfigMap from a Pod  [practice · configuration]
namespace: ckad-configmap-consume-czmva  (seed 7)

In `ckad-configmap-consume-czmva`, expose only key `LOG_LEVEL` of ConfigMap
`app-config` as the environment variable `LOG_LEVEL` in a new Pod `consumer-163`
(image `busybox:1.36`, command `sleep 3600`).

hints:
  - ...
```

Do the work **in that namespace** with `kubectl`/YAML, just like the exam. Then:

```bash
ckad-trainer check configmap-consume      # per-assertion PASS/FAIL table
```

```
Pod/consumer-163
  [PASS] {.spec.containers[0].env[?(@.name=="LOG_LEVEL")].valueFrom.configMapKeyRef.name}  want=app-config  got=app-config
  [PASS] {.spec.containers[0].env[?(@.name=="LOG_LEVEL")].valueFrom.configMapKeyRef.key}   want=LOG_LEVEL   got=LOG_LEVEL

PASS — Consume a ConfigMap from a Pod
```

`check` is re-runnable — iterate until it's green. Assertions that depend on the
cluster settling (a PVC becoming `Bound`, a Deployment becoming ready) are polled
for a while; a missing object fails immediately.

When you're done — or want to give up — see the answer and tear down:

```bash
ckad-trainer solution configmap-consume   # the reference YAML/commands + an explanation
ckad-trainer solve    configmap-consume   # actually APPLY the reference answer (to inspect it)
ckad-trainer cleanup  configmap-consume   # delete the namespace + any cluster-scoped objects
```

To start over with a fresh random draw of the same scenario:

```bash
ckad-trainer reset configmap-consume      # = cleanup + start again
```

Don't care which scenario? Let it pick:

```bash
ckad-trainer random                       # any scenario
ckad-trainer random --domain security     # any scenario from one domain
```

---

## 4. Randomized scenarios and seeds

Scenarios marked **🎲** in `list` are randomized so you drill the *API*, not one
memorized answer. Two kinds of variation:

- **Different values each run** — replicas, ports, `runAsUser`, storage sizes, a
  probe path. You have to actually *read* the task.
- **Different variant each run** — e.g. a ConfigMap task that sometimes wants
  `envFrom`, sometimes a single `configMapKeyRef`, sometimes a volume mount.

The draw is saved when you `start`, so `check`, `solution`, and `cleanup` all
refer to the same task. To replay a specific draw (handy for comparing notes or
re-attempting the exact task):

```bash
ckad-trainer start pv-pvc-pod-static --seed 42
```

The same seed always produces the same variant and values.

---

## 5. Managing active scenarios

Started something in one shell and lost track of it? List everything that's live:

```bash
ckad-trainer status
```

```
SCENARIO                   NAMESPACE                       VARIANT      AGE
configmap-consume          ckad-configmap-consume-czmva    single-key   4m
tolerations                ckad-tolerations-d1wbh          -            2m
```

Re-show the full task for one (read-only, instant — no cluster calls):

```bash
ckad-trainer status configmap-consume
```

Tear down a single scenario, or everything at once:

```bash
ckad-trainer cleanup configmap-consume    # one
ckad-trainer cleanup --all                # every active scenario (and ends any exam)
```

---

## 6. Flashcard drills

`drill` is a quick recall trainer for the fiddly `kubectl` command formats
(jsonpath, `--sort-by`, `custom-columns`, field selectors, `wait --for`, ...).
It shuffles the flashcards, shows each prompt, waits for you to type your answer,
then reveals the canonical command. No cluster needed.

```bash
ckad-trainer drill
```

```
drill [1/10]  observability
List the Pods in the current namespace sorted by their container restart count.
> kubectl get pods --sort-by=...
answer: kubectl get pods --sort-by='.status.containerStatuses[0].restartCount'
```

---

## 7. Exam mode

A timed, domain-weighted session. It samples N tasks (heavier domains get more),
starts them all, and runs a clock.

```bash
ckad-trainer exam start --count 16 --minutes 120   # both default from config
```

It prints every task (each in its own namespace) as your "exam paper". Work them
in any order. Check progress without grading:

```bash
ckad-trainer exam status      # Time left: 47m12s | passing: 9/16 tasks
```

When you're done (or time's up), grade and tear everything down:

```bash
ckad-trainer exam grade
```

```
Tasks:
  [PASS] resource-requests-limits   App Config & Security
  [FAIL] service-expose             Services & Networking
  ...
By domain:
  App Config & Security          6/7
  Services & Networking          1/2
  ...
Score: 78%  (12/16 tasks, domain-weighted)
```

The score is weighted by the official 2026 domain percentages, so a miss in a
small-but-heavy domain costs more — just like the real exam. To bail out without
scoring:

```bash
ckad-trainer exam abort
```

---

## 8. Command reference

| Command | What it does |
|---------|--------------|
| `init [--context <name>] [--force]` | Write `config.yaml` pinned to a kube context (default: current) |
| `doctor` | Check kubectl, the safety context guard, and reachability |
| `list` | List the scenario catalog (🎲 = randomized) |
| `start <id> [--seed N] [--force]` | Set up a scenario and print the task |
| `status [<id>]` | List active scenarios, or re-show one task |
| `check <id>` | Verify your work; per-assertion PASS/FAIL table |
| `solution <id>` | Show the reference answer (YAML/commands + explanation) |
| `solve <id>` | Apply the reference answer to the cluster (to inspect it) |
| `cleanup <id>` / `cleanup --all` | Tear down one / every active scenario |
| `reset <id>` | Clean up and restart with a fresh draw |
| `random [--domain <slug>] [--seed N]` | Start a random scenario |
| `drill` | Flashcard recall drills for kubectl command formats |
| `exam start [--count N] [--minutes M] [--seed N]` | Begin a timed exam |
| `exam status` | Time left + tasks passing |
| `exam grade` | Score, per-domain breakdown, and clean up |
| `exam abort` | End the exam without scoring |

Global flag: `--config <path>`. With no flag the config is found at
`./config.yaml` if present, otherwise the per-user
`$XDG_CONFIG_HOME/ckad-trainer/config.yaml`. Color output turns off
automatically when piped or when `NO_COLOR` is set.

---

## 9. Configuration

A single `config.yaml` (copy `config.example.yaml`):

```yaml
cluster:
  provider: minikube     # minikube | kubeconfig
  context: minikube      # the kube context to operate on (REQUIRED)
  kubectl: kubectl       # kubectl binary to use
  auto_start: true       # minikube only: start it if down
namespace_prefix: ckad   # scenario namespaces are <prefix>-<id>-<rand>
defaults:
  exam:
    count: 16            # default `exam start --count`
    minutes: 120         # default `exam start --minutes`
safety:
  require_context: minikube   # the ONLY context the app may touch (REQUIRED)
# scenario_dir: ./scenarios   # load scenarios from disk instead of the embedded catalog
```

- **Any** Kubernetes works: set `provider: kubeconfig` and point `context` at a
  kind, Docker Desktop, or remote context.
- `safety.require_context` is a guardrail — the app refuses to run unless your
  current context matches it. Keep it pointed at a throwaway cluster.
- `scenario_dir` is only for authoring (see the README); leave it commented to
  use the catalog baked into the binary.
- Active-scenario and exam state are kept under `$XDG_STATE_HOME/ckad-trainer`
  (default `~/.local/state/ckad-trainer`; override with `CKAD_TRAINER_STATE`), so
  `status`/`check`/`cleanup` find your work regardless of the current directory.

---

## 10. Scenario catalog

70 scenarios — 47 hands-on (most randomized) plus 23 kubectl/helm flashcards —
mapped to the 2026 CKAD domains:

| Domain (weight) | Covered |
|-----------------|---------|
| **App Environment, Config & Security** (25%) | ConfigMaps (`envFrom`/keyRef/volume/`items`/immutable/subPath), Secrets (env/volume/docker-registry), resource requests/limits, ResourceQuota, LimitRange, securityContext (runAsUser/Group, fsGroup, caps add/drop, readOnlyRootFS), ServiceAccount, RBAC (Role + ClusterRole, RoleBinding/ClusterRoleBinding, `auth can-i`), static PV→PVC→Pod, dynamic PVC provisioning |
| **Application Design & Build** (20%) | multi-container + init + native sidecar + ambassador proxy (emptyDir/localhost), Jobs (fields, Indexed/TTL), CronJob, command/args, downward API, PriorityClass, node scheduling (nodeSelector/tolerations) |
| **Application Deployment** (20%) | Deployments + scale, `set image` rollouts, rollout undo/pause, update strategies, canary, blue/green, HPA |
| **Services & Networking** (20%) | Service ClusterIP/NodePort, endpoints debugging, NetworkPolicy ingress/egress, Ingress routing + TLS termination |
| **Observability & Maintenance** (15%) | liveness/readiness/startup probes, fix-a-broken-probe, ephemeral debug containers |
| **Flashcards** | context switch + default namespace (`config use-context`/`set-context`), jsonpath, `--sort-by`, custom-columns, field selectors, `wait --for`, set-based selectors, TLS secret, create token, `auth can-i`, rollout restart, debug syntax, logs (`--previous`), `top`, events `--sort-by`, api-versions/deprecations, image build, Helm (install/upgrade/pull), Kustomize, CRD discovery |

`ckad-trainer list` shows them all; `ckad-trainer random --domain <slug>` filters
by the fine-grained slugs (`configuration`, `core-concepts`, `multi-container`,
`observability`, `pod-design`, `security`, `services-networking`,
`state-persistence`).

---

## 11. Tips and troubleshooting

- **`doctor` says "context guard" failed.** Your current kube context isn't the
  one in `safety.require_context`. Run `kubectl config use-context <name>` (or fix
  the config) and re-run `doctor`.
- **A `check` keeps failing on a status field.** Some assertions wait for the
  object to settle (Bound/ready/endpoints). If the object doesn't exist at all,
  the check fails right away — make sure you created it in the **scenario's**
  namespace (shown by `start`/`status`), not `default`.
- **Lost your task across shells.** `ckad-trainer status` lists everything;
  `ckad-trainer status <id>` re-prints the task.
- **Want a clean slate.** `ckad-trainer cleanup --all` removes every scenario's
  namespace and tracked cluster-scoped objects, and ends any exam.
- **Practice reading, not memorizing.** Re-`start` (or `reset`) randomized
  scenarios a few times — the values and variant change, so you have to read the
  task each time, which is exactly the exam skill.
