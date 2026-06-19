# CKAD Trainer

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](./LICENSE.md)

<img width="1024" height="572" alt="banner" src="https://github.com/user-attachments/assets/034fe782-2102-4b45-b347-f56aa2f86d80" />

A local, self-contained practice app for the **Certified Kubernetes Application
Developer (CKAD)** exam. It runs hands-on scenarios against *your own* Kubernetes
cluster: it sets up the starting state, gives you a task, checks your work with a
per-assertion **PASS/FAIL** table, and cleans up after itself.

Built to grind the muscle memory the exam actually tests — imperative commands,
specific API fields, and "modify this existing object" tasks under time pressure.
**70 scenarios** (47 hands-on, most randomized, + 23 kubectl/helm flashcards) across
all CKAD domains, in both a practice mode and a timed, scored exam mode.

## Table of contents

- [What it is](#what-it-is)
- [Install](#install)
- [Quickstart](#quickstart)
- [Using it](#using-it)
- [Building and developing](#building-and-developing)
  - [Requirements](#requirements)
  - [Build and install](#build-and-install)
  - [Running the tests](#running-the-tests)
  - [Project layout](#project-layout)
  - [Adding a scenario](#adding-a-scenario)
  - [Design](#design)
- [Configuration](#configuration)
- [Disclaimer](#disclaimer)
- [License](#license)

---

## What it is

Each scenario runs in its own throwaway namespace:

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

- **Practice mode** — small, focused tasks; most are **randomized** (different
  values *and* different variants each run) so you drill the API instead of one
  memorized answer. Seedable for reproducibility.
- **Exam mode** — a timed session that samples tasks weighted by the official
  2026 domain breakdown and scores you with a per-domain "where you're weak"
  report.

Everything the app creates is labeled `app.kubernetes.io/managed-by=ckad-trainer`,
and it only ever touches the kube context you allow-list, so your cluster always
goes back to clean.

---

## Install

Download the binary for your platform from the
[latest release](https://github.com/franciscopaglia/ckad-trainer/releases/latest),
make it executable, and put it on your `PATH`:

```bash
# Linux (x86_64)
curl -fsSL -o ckad-trainer https://github.com/franciscopaglia/ckad-trainer/releases/latest/download/ckad-trainer-linux-amd64
chmod +x ckad-trainer
sudo mv ckad-trainer /usr/local/bin/      # somewhere on your PATH

ckad-trainer --version                    # confirm it's installed
```

Pick the asset that matches your machine:

| Platform | Asset |
|----------|-------|
| Linux x86_64 | `ckad-trainer-linux-amd64` |
| Linux ARM64 | `ckad-trainer-linux-arm64` |
| macOS Intel | `ckad-trainer-darwin-amd64` |
| macOS Apple Silicon | `ckad-trainer-darwin-arm64` |
| Windows x86_64 | `ckad-trainer-windows-amd64.exe` |

Each release ships a `SHA256SUMS` file. To verify your download:

```bash
curl -fsSLO https://github.com/franciscopaglia/ckad-trainer/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing      # expect "<asset>: OK"
```

macOS users: if Gatekeeper blocks the binary, clear the quarantine flag with
`xattr -d com.apple.quarantine ckad-trainer`. Or [build from source](#build-and-install).

**With the Go toolchain** you can skip the download entirely:

```bash
go install github.com/franciscopaglia/ckad-trainer/cmd/ckad-trainer@latest
```

This drops `ckad-trainer` in `$(go env GOPATH)/bin`. The catalog is embedded, so
the binary is self-contained; it stores config and progress under
`$XDG_CONFIG_HOME` / `$XDG_STATE_HOME` (see [Configuration](#configuration)), so
it works from any directory.

---

## Quickstart

```bash
ckad-trainer init                     # writes config.yaml from your current kube context
ckad-trainer doctor                   # checks kubectl, the safety guard, reachability

ckad-trainer start configmap-consume    # set up a task + print it
#   ... do the work with kubectl/YAML ...
ckad-trainer check  configmap-consume   # PASS/FAIL table
ckad-trainer cleanup configmap-consume  # tear it down
```

> **No setup needed:** with no `config.yaml`, ckad-trainer just uses whatever
> context `kubectl` is currently pointed at. Until you run `init` there's no
> pinned context, so the safety guard is inactive — the app follows your current
> context each time, command to command. `init` writes that choice to
> `config.yaml` and pins the safety guard to it, so a later `kubectl` context
> switch can't make the app run somewhere you didn't expect. Target a different
> cluster with `ckad-trainer init --context <name>`. Works with minikube
> (`scripts/minikube-up.sh`), kind, or any other context.

---

## Using it

**See the [Usage guide](./USAGE.md)** for the full walkthrough: the practice
loop (`start`/`check`/`solution`/`solve`/`cleanup`/`reset`), randomization and
`--seed`, tracking active scenarios (`status`, `cleanup --all`), flashcard
`drill`s, timed `exam` sessions and scoring, every command, and tips.

A taste:

```bash
ckad-trainer list                     # browse the catalog (🎲 = randomized)
ckad-trainer random --domain security
ckad-trainer status                   # what's active across your shells
ckad-trainer drill                    # kubectl command-format flashcards
ckad-trainer exam start --count 16 --minutes 120
```

---

## Building and developing

### Requirements

- A **Kubernetes cluster** you don't mind scribbling on (minikube, kind,
  Docker Desktop, or any other local/remote cluster) and **`kubectl`**.
- A recent **Go toolchain** (1.24+; the module pins a newer toolchain that the
  `go` command fetches automatically).

### Build and install

The scenario catalog is embedded into the binary with `go:embed`, so the build
is a single self-contained executable.

```bash
make build              # -> ./ckad-trainer
make install            # -> $(PREFIX)/bin/ckad-trainer   (PREFIX defaults to /usr/local)
make help               # list all targets
```

### Running the tests

```bash
make test     # cluster-free: unit tests + loads & renders the whole catalog across seeds
make check    # fmt + vet + test  (the pre-commit gate)
make smoke    # cluster-backed: starts every scenario x3 seeds, applies its own
              # solution, and asserts the check PASSes (mutates the configured cluster)
```

`make smoke` is the strongest guarantee — it proves every scenario is solvable
across its random draws. It runs against the cluster in your `config.yaml` and is
gated behind a build tag (`go test -tags=cluster`) so the default `go test ./...`
stays cluster-free.

### Project layout

```
cmd/ckad-trainer/        # CLI entrypoint (cobra)
internal/
  config/                # config.yaml loading + validation
  cluster/               # doctor checks + the mutation safety guard
  kubectl/               # thin kubectl exec wrapper (always injects --context)
  scenario/              # YAML scenario schema, loader, validator
  engine/                # start/check/cleanup, randomization, solution apply
  verify/                # JSONPath assertion evaluation (client-go engine)
  exam/                  # timed, domain-weighted sessions + scoring
catalog.go               # go:embed of the scenarios/ directory
scenarios/
  practice/<domain>/*.yaml   # hands-on scenarios (data, not code)
  flashcards/*.yaml          # kubectl command-format recall drills
config.example.yaml      Makefile      USAGE.md
```

> Engine internals and invariants are documented in
> [`internal/engine/CLAUDE.md`](./internal/engine/CLAUDE.md); see also the root
> [`CLAUDE.md`](./CLAUDE.md) for an orientation.

### Adding a scenario

Scenarios are plain **YAML data** — add a file under `scenarios/practice/<domain>/`
and the engine picks it up; no Go changes needed. Each defines `setup`, a
`prompt`, declarative `verify` assertions, a `solution`, and (for cluster-scoped
objects) `cleanup`. Randomized scenarios add `params` and/or `variants`.

While authoring, set `scenario_dir: ./scenarios` in `config.yaml` to load from
disk instead of the embedded copy, then validate with `make test` and prove it
solvable with `make smoke`. The schema is the Go types in
[`internal/scenario/scenario.go`](./internal/scenario/scenario.go) (each field
is YAML-tagged and commented); the files under `scenarios/` are worked examples.

### Design

The architecture, scenario schema, randomization model, and check semantics are
documented next to the code: [`internal/engine/CLAUDE.md`](./internal/engine/CLAUDE.md)
for the engine, package doc comments elsewhere, and the root
[`CLAUDE.md`](./CLAUDE.md) for an orientation.

---

## Configuration

A single `config.yaml`. `ckad-trainer init` writes it for you (pinned to your
current context); the file is resolved in this order:

1. `--config <path>`, if given;
2. `./config.yaml` in the working directory (handy for development/authoring);
3. the per-user file `$XDG_CONFIG_HOME/ckad-trainer/config.yaml` (default
   `~/.config/ckad-trainer/config.yaml`).

Active-scenario and exam state live under `$XDG_STATE_HOME/ckad-trainer`
(default `~/.local/state/ckad-trainer`; override with `CKAD_TRAINER_STATE`), so
the installed binary tracks your progress the same from any directory.

The `safety.require_context` guard refuses to run unless your current kube
context matches it, so the app never touches a real cluster by accident.

```yaml
cluster:
  provider: kubeconfig   # minikube | kubeconfig
  context: minikube      # the context to operate on
  kubectl: kubectl
namespace_prefix: ckad
defaults:
  exam: { count: 16, minutes: 120 }
safety:
  require_context: minikube
```

Full field-by-field reference in the [Usage guide](./USAGE.md#9-configuration).

---

## Disclaimer

**Independent project — not affiliated with, authorized by, or endorsed by The
Linux Foundation or the Cloud Native Computing Foundation (CNCF).** This is a
free, community-built study aid for the CKAD exam, not part of the official
certification program.

**No real exam questions.** This repository contains **no actual CKAD exam
content**. Every scenario and flashcard is original, written from the publicly
published [CKAD curriculum](https://github.com/cncf/curriculum) and from
community blog posts, write-ups, and exam testimonials describing the *kinds* of
tasks the exam covers. Nothing here reproduces confidential exam material, so
using it does not violate the Linux Foundation's exam confidentiality agreement.
The scenarios reflect those public sources and may not match the current exam in
content, format, or difficulty — no passing guarantee is implied.

**Trademarks.** "CKAD", "Certified Kubernetes Application Developer",
"Kubernetes", and "CNCF" are trademarks of The Linux Foundation / Cloud Native
Computing Foundation. They are used here only descriptively, to identify the exam
this tool helps you prepare *for*, and their use does not imply any endorsement.

---

## License

Licensed under the **GNU General Public License v3.0 (or later)** — see
[LICENSE.md](./LICENSE.md). Each source file carries an
`SPDX-License-Identifier: GPL-3.0-or-later` header.
