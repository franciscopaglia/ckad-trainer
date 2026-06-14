# CKAD Trainer

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](./LICENSE.md)

A local, self-contained practice app for the **Certified Kubernetes Application
Developer (CKAD)** exam. It runs hands-on scenarios against *your own*
Kubernetes cluster: it sets up the starting state, gives you a task, checks your
work, tells you **PASS** or **FAIL** with a per-check breakdown, and cleans up
after itself.

Built to grind the muscle memory the exam actually tests — imperative commands,
specific API fields, and "modify this existing object" tasks under time pressure.

> Status: **working.** Practice + exam modes are implemented with a catalog of
> ~30 scenarios across 8 domains (most randomized). See [PLAN.md](./PLAN.md) for
> the design and phase history.

---

## Quickstart

```bash
make build                       # builds ./ckad-trainer (catalog embedded)
cp config.example.yaml config.yaml   # then edit: set your kube context
./ckad-trainer doctor            # checks kubectl, the context guard, reachability

./ckad-trainer list                       # browse the catalog (🎲 = randomized)
./ckad-trainer start configmap-consume    # set up a task + print it
#   ... do the work with kubectl/YAML ...
./ckad-trainer check  configmap-consume   # PASS/FAIL table
./ckad-trainer solution configmap-consume # reveal the answer
./ckad-trainer cleanup configmap-consume  # tear it down

./ckad-trainer random                     # start a random scenario
./ckad-trainer start pv-pvc-pod-static --seed 42   # reproducible draw
./ckad-trainer drill                      # kubectl command-format flashcards

./ckad-trainer exam start --count 16 --minutes 120
./ckad-trainer exam status                # time left + tasks passing
./ckad-trainer exam grade                 # weighted score + per-domain breakdown
```

> **Cluster note:** any Kubernetes context works — set `provider: kubeconfig`
> and `context` in `config.yaml`. For minikube, `scripts/minikube-up.sh` brings a
> cluster up; kind or any other local cluster works just as well. The
> `require_context` guard keeps the app from touching anything but the context you
> name.

---

## Two modes

### Practice mode
Many small, focused scenarios. Each one isolates a specific piece of the API —
a ConfigMap consumed via `envFrom`, a Secret mounted as a volume, a
`readinessProbe` with the right `initialDelaySeconds`, a PV→PVC→Pod chain, a
default-deny NetworkPolicy. Some ask you to build from scratch; some hand you a
half-built object to fix. You work in the cluster, then ask the app to check it.

**Some scenarios are randomized** so you drill the *API*, not one memorized
answer. Two flavors:
- **Different values each run** — replicas, ports, `runAsUser`, storage size, a
  probe path. You have to actually read the task.
- **Different variant each run** — e.g. a ConfigMap task that sometimes wants
  `envFrom`, sometimes a single `configMapKeyRef`, sometimes a volume mount.

The check adapts to whatever was drawn, and the draw is saved so re-checking is
consistent. Pass `--seed N` to replay a specific one. Other scenarios (debug/fix
tasks) are intentionally hard-coded.

### Exam mode
A timed session (default ~16 tasks / 120 min) that samples scenarios weighted by
the real exam's domain breakdown, lets you jump between tasks, then grades
everything at the end with a score and a per-domain "where you're weak" report.
Tasks are modeled on the *style* of real exam questions — original wording, same
shape.

---

## How a scenario works

```
start  ──▶  app creates a fresh namespace + any starting objects, shows the task
            │
            ▼
        you do the work with kubectl / YAML, just like the exam
            │
check  ──▶  app inspects the live cluster and prints a PASS/FAIL table
            │
cleanup ─▶  app deletes the namespace and any cluster-scoped objects it tracked
```

Everything the app creates is labeled `app.kubernetes.io/managed-by=ckad-trainer`,
so cleanup can always find and remove its own objects — your cluster goes back to
clean.

---

## Requirements

- A Kubernetes cluster you don't mind getting scribbled on. **minikube** is the
  default target; any kubeconfig context works.
- `kubectl`
- Go 1.22+ to build (ships as a single static binary with the scenario catalog
  embedded — see PLAN.md §4)

> Neither `minikube` nor `kubectl` is installed on this machine yet — the first
> build phase adds a `doctor` command and a minikube setup helper.

---

## Quickstart (target UX)

```bash
# one-time: copy and edit the config
cp config.example.yaml config.yaml

# verify your environment and cluster
ckad-trainer doctor

# practice
ckad-trainer list --domain configuration
ckad-trainer start  configmap-envfrom     # sets up + prints the task
#   ... you work in the cluster ...
ckad-trainer check  configmap-envfrom      # PASS/FAIL breakdown
ckad-trainer solution configmap-envfrom    # reference answer + explanation
ckad-trainer cleanup configmap-envfrom

# pick something at random
ckad-trainer random

# exam simulation
ckad-trainer exam start --count 16 --minutes 120
ckad-trainer exam status
ckad-trainer exam grade
```

---

## Configuration

A single `config.yaml`. A safety guardrail (`require_context`) refuses to run
unless your current kube context matches what you expect, so the app never
touches a real cluster by accident.

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
```

See `config.example.yaml`.

---

## Scenario coverage

Scenarios map to the 2026 CKAD domains:

| Domain                                            | Weight | Examples |
|---------------------------------------------------|:------:|----------|
| App Environment, Configuration & Security         | 25%    | ConfigMaps, Secrets, SecurityContext, ServiceAccounts, ResourceQuota/LimitRange, RBAC, resource requests/limits |
| Application Design and Build                       | 20%    | Multi-container pods, init/sidecar, Jobs/CronJobs, downwardAPI, images |
| Application Deployment                             | 20%    | Deployments, scaling, rollouts/undo, strategies, canary, Helm |
| Services and Networking                           | 20%    | ClusterIP/NodePort, selectors, NetworkPolicy, Ingress |
| Application Observability and Maintenance          | 15%    | liveness/readiness/startup probes, logs/events debugging, `top` |

Full catalog and authoring schema in [PLAN.md](./PLAN.md).

---

## Project layout

```
PLAN.md              # design + roadmap
README.md            # this file
config.example.yaml  # sample config
trainer/             # the CLI app
scenarios/           # scenario definitions (YAML data, not code)
  practice/<domain>/
  exam/<set>/
```

---

## Contributing scenarios

Scenarios are plain YAML data files — add one without touching the engine. Each
defines `setup`, a `prompt`, declarative `verify` assertions, a `solution`, and
`cleanup`. Schema and examples in [PLAN.md](./PLAN.md) §5.

---

## License

Licensed under the **GNU General Public License v3.0 (or later)** — see
[LICENSE.md](./LICENSE.md). Each source file carries an
`SPDX-License-Identifier: GPL-3.0-or-later` header.
