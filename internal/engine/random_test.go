// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

func sampleScenario() scenario.Scenario {
	return scenario.Scenario{
		ID: "t", Mode: scenario.ModePractice, Domain: "configuration",
		Params: map[string]scenario.Param{
			"a": {Pick: []string{"x", "y", "z"}},
			"n": {Range: &scenario.Range{Min: 1, Max: 5}},
		},
		Variants: []scenario.Variant{
			{Name: "v1", Weight: 1, Prompt: "p1"},
			{Name: "v2", Weight: 1, Prompt: "p2"},
		},
	}
}

func TestDrawDeterministic(t *testing.T) {
	s := sampleScenario()
	v1, p1, err := draw(s, rand.New(rand.NewSource(7)))
	if err != nil {
		t.Fatal(err)
	}
	v2, p2, _ := draw(s, rand.New(rand.NewSource(7)))
	if v1 != v2 {
		t.Errorf("variant differs for same seed: %q vs %q", v1, v2)
	}
	if !reflect.DeepEqual(p1, p2) {
		t.Errorf("params differ for same seed: %v vs %v", p1, p2)
	}
}

func TestDrawVariesAcrossSeeds(t *testing.T) {
	s := sampleScenario()
	seen := map[string]bool{}
	for seed := int64(1); seed <= 20; seed++ {
		_, p, _ := draw(s, rand.New(rand.NewSource(seed)))
		seen[p["a"]] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected param 'a' to vary across seeds, only saw %v", seen)
	}
}

// TestRenderCheckDoesNotMutate is a regression test: renderCheck must not bake
// rendered values back into the shared scenario (slice-aliasing bug).
func TestRenderCheckDoesNotMutate(t *testing.T) {
	c := scenario.Check{
		Kind: "Pod", Name: "{{.a}}",
		Assert: []scenario.Assert{{Path: "{.x}", Equals: "{{.a}}", In: []string{"{{.a}}"}}},
	}
	if _, err := renderCheck(c, map[string]any{"a": "app"}); err != nil {
		t.Fatal(err)
	}
	if c.Name != "{{.a}}" || c.Assert[0].Equals != "{{.a}}" || c.Assert[0].In[0] != "{{.a}}" {
		t.Errorf("renderCheck mutated its input: name=%q equals=%q in=%q",
			c.Name, c.Assert[0].Equals, c.Assert[0].In[0])
	}
}

// TestRenderHints covers the fix for hints printed with raw {{.param}}
// placeholders: hints must render with the instance's draw, like the prompt.
func TestRenderHints(t *testing.T) {
	s := scenario.Scenario{
		ID: "t", Mode: scenario.ModePractice, Domain: "configuration",
		Prompt: "p",
		Hints:  []string{"kubectl get pod -n {{.ns}}", "scale to {{.replicas}}"},
	}
	inst := &Instance{ScenarioID: "t", Namespace: "ns-1", Params: map[string]string{"replicas": "3"}}
	hints, err := RenderHints(s, inst)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kubectl get pod -n ns-1", "scale to 3"}
	if !reflect.DeepEqual(hints, want) {
		t.Errorf("rendered hints = %v, want %v", hints, want)
	}
}
