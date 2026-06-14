// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package engine drives a scenario's lifecycle: start -> (user works) -> check
// -> cleanup. It resolves a scenario into a concrete Instance, sets up cluster
// state in an isolated namespace, verifies the user's work, and tears it down.
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/franciscopaglia/ckad-trainer/internal/cluster"
	"github.com/franciscopaglia/ckad-trainer/internal/config"
	"github.com/franciscopaglia/ckad-trainer/internal/kubectl"
	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

// Label keys applied to everything the app creates (PLAN.md §4c).
const (
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelScenario  = "ckad-trainer/scenario"
	LabelRun       = "ckad-trainer/run"
	ManagedByValue = "ckad-trainer"
)

// stateDir holds one JSON file per active scenario.
const stateDir = "state"

// ErrAlreadyStarted is returned by Start when a scenario already has live state.
var ErrAlreadyStarted = errors.New("scenario already started (use --force to restart)")

// ObjRef names a cluster-scoped object created/owned by a run.
type ObjRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Instance is the resolved draw for one run; persisted to state/<id>.json.
type Instance struct {
	ScenarioID    string            `json:"scenario_id"`
	RunID         string            `json:"run_id"`
	Seed          int64             `json:"seed"`
	Variant       string            `json:"variant"`
	Namespace     string            `json:"namespace"`
	Params        map[string]string `json:"params"`
	ClusterScoped []ObjRef          `json:"cluster_scoped"`
	StartedAt     time.Time         `json:"started_at"`
}

// Start resolves the scenario, creates its namespace and setup state, persists
// the Instance, and returns it. Use the rendered prompt via RenderPrompt.
func Start(cfg *config.Config, s scenario.Scenario, seed int64) (*Instance, error) {
	kc := kubectl.New(cfg.Cluster.Kubectl, cfg.Cluster.Context)
	if err := cluster.Guard(cfg, kc); err != nil {
		return nil, err
	}
	if _, err := os.Stat(statePath(s.ID)); err == nil {
		return nil, ErrAlreadyStarted
	}

	// A seed of 0 means "pick one for me"; a non-zero seed makes the draw
	// reproducible (e.g. `start --seed 42`).
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))
	variant, params, err := draw(s, rng)
	if err != nil {
		return nil, err
	}

	runID := randID(5)
	ns := makeNamespace(cfg.NamespacePrefix, s.ID, runID)

	inst := &Instance{
		ScenarioID: s.ID,
		RunID:      runID,
		Seed:       seed,
		Variant:    variant,
		Namespace:  ns,
		Params:     params,
		StartedAt:  time.Now().UTC(),
	}
	data := inst.data()

	// Resolve cluster-scoped cleanup targets from the (possibly templated) names.
	for _, ref := range s.Cleanup.ClusterScoped {
		name, err := render(ref.Name, data)
		if err != nil {
			return nil, fmt.Errorf("rendering cleanup name: %w", err)
		}
		inst.ClusterScoped = append(inst.ClusterScoped, ObjRef{Kind: ref.Kind, Name: name})
	}

	labels := map[string]string{
		LabelManagedBy: ManagedByValue,
		LabelScenario:  s.ID,
		LabelRun:       runID,
	}
	if err := kc.CreateNamespace(ns, labels); err != nil {
		return nil, fmt.Errorf("creating namespace: %w", err)
	}

	// Apply setup manifests (rendered + labeled).
	for _, m := range s.Setup.Manifests {
		rendered, err := render(m, data)
		if err != nil {
			return nil, fmt.Errorf("rendering setup manifest: %w", err)
		}
		labeled, err := injectLabels(rendered, labels)
		if err != nil {
			return nil, fmt.Errorf("labeling setup manifest: %w", err)
		}
		if err := kc.Apply(labeled); err != nil {
			return nil, err
		}
	}
	// Run setup commands.
	for _, c := range s.Setup.Commands {
		rendered, err := render(c, data)
		if err != nil {
			return nil, fmt.Errorf("rendering setup command: %w", err)
		}
		args := strings.Fields(rendered)
		if len(args) > 0 && args[0] == "kubectl" {
			args = args[1:]
		}
		if _, err := kc.Raw(args...); err != nil {
			return nil, err
		}
	}

	if err := writeState(inst); err != nil {
		return nil, err
	}
	return inst, nil
}

// Cleanup deletes the scenario namespace, any tracked cluster-scoped objects, and
// the state file.
func Cleanup(cfg *config.Config, inst *Instance) error {
	kc := kubectl.New(cfg.Cluster.Kubectl, cfg.Cluster.Context)
	if err := cluster.Guard(cfg, kc); err != nil {
		return err
	}
	if err := kc.DeleteNamespace(inst.Namespace); err != nil {
		return err
	}
	for _, ref := range inst.ClusterScoped {
		if err := kc.Delete(ref.Kind, ref.Name, ""); err != nil {
			return err
		}
	}
	return os.Remove(statePath(inst.ScenarioID))
}

// RenderPrompt returns the task text for the instance's chosen variant (or the
// top-level prompt when there are no variants).
func RenderPrompt(s scenario.Scenario, inst *Instance) (string, error) {
	prompt := s.Prompt
	if v, ok := findVariant(s, inst.Variant); ok {
		prompt = v.Prompt
	}
	return render(prompt, inst.data())
}

// findVariant returns the named variant, or (zero, false) when name is empty or
// not found.
func findVariant(s scenario.Scenario, name string) (scenario.Variant, bool) {
	if name == "" {
		return scenario.Variant{}, false
	}
	for _, v := range s.Variants {
		if v.Name == name {
			return v, true
		}
	}
	return scenario.Variant{}, false
}

// LoadInstance reads the persisted instance for a scenario id.
func LoadInstance(id string) (*Instance, error) {
	raw, err := os.ReadFile(statePath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("scenario %q is not started (run `start %s` first)", id, id)
		}
		return nil, err
	}
	var inst Instance
	if err := json.Unmarshal(raw, &inst); err != nil {
		return nil, fmt.Errorf("reading state for %q: %w", id, err)
	}
	return &inst, nil
}

// HasState reports whether a scenario currently has live state.
func HasState(id string) bool {
	_, err := os.Stat(statePath(id))
	return err == nil
}

// --- helpers ---

// data builds the template context for rendering.
func (inst *Instance) data() map[string]any {
	d := map[string]any{
		"ns":      inst.Namespace,
		"variant": inst.Variant,
	}
	for k, v := range inst.Params {
		d[k] = v
	}
	return d
}

func statePath(id string) string { return filepath.Join(stateDir, id+".json") }

func writeState(inst *Instance) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(inst.ScenarioID), raw, 0o644)
}

// render executes a text/template, failing on any missing key.
func render(tmpl string, data map[string]any) (string, error) {
	t, err := template.New("t").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// makeNamespace builds a DNS-1123 namespace name, truncated to 63 chars.
func makeNamespace(prefix, id, runID string) string {
	raw := strings.ToLower(fmt.Sprintf("%s-%s-%s", prefix, id, runID))
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	ns := b.String()
	if len(ns) > 63 {
		ns = ns[:63]
	}
	return strings.Trim(ns, "-")
}

const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func randID(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = idAlphabet[rand.Intn(len(idAlphabet))]
	}
	return string(b)
}

// injectLabels parses a single-document YAML manifest, merges in labels under
// metadata.labels, and re-serializes it.
func injectLabels(manifest string, labels map[string]string) (string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(manifest), &doc); err != nil {
		return "", err
	}
	meta, _ := doc["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		doc["metadata"] = meta
	}
	lbls, _ := meta["labels"].(map[string]any)
	if lbls == nil {
		lbls = map[string]any{}
		meta["labels"] = lbls
	}
	for k, v := range labels {
		lbls[k] = v
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
