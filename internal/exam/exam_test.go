// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package exam

import (
	"math"
	"math/rand"
	"testing"

	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

func TestSummarizeWeightedScore(t *testing.T) {
	byDomain := map[string]*DomainScore{
		"App Config & Security": {Domain: "App Config & Security", Passed: 4, Total: 5},
		"Services & Networking": {Domain: "Services & Networking", Passed: 0, Total: 1},
	}
	domains, score := summarize(byDomain)
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domains))
	}
	// (25*0.8 + 20*0) / (25+20) = 44.44%
	if math.Abs(score-44.44) > 0.5 {
		t.Errorf("weighted score = %.2f, want ~44.4", score)
	}
}

func TestSummarizePerfectScore(t *testing.T) {
	byDomain := map[string]*DomainScore{
		"Observability & Maintenance": {Domain: "Observability & Maintenance", Passed: 2, Total: 2},
	}
	if _, score := summarize(byDomain); math.Abs(score-100) > 0.01 {
		t.Errorf("all-pass score = %.2f, want 100", score)
	}
}

func TestSampleWeightsTowardHeavyDomains(t *testing.T) {
	pool := []scenario.Scenario{
		{ID: "a", Domain: "configuration"}, // weight 25
		{ID: "b", Domain: "observability"}, // weight 15
	}
	countA := 0
	const n = 4000
	for seed := int64(0); seed < n; seed++ {
		out := sample(pool, 1, rand.New(rand.NewSource(seed)))
		if out[0].ID == "a" {
			countA++
		}
	}
	frac := float64(countA) / n // expected 25/40 = 0.625
	if math.Abs(frac-0.625) > 0.05 {
		t.Errorf("heavy-domain pick fraction = %.3f, want ~0.625", frac)
	}
}

func TestSampleWithoutReplacementCapsAtPool(t *testing.T) {
	pool := []scenario.Scenario{
		{ID: "a", Domain: "configuration"},
		{ID: "b", Domain: "observability"},
	}
	out := sample(pool, 5, rand.New(rand.NewSource(1)))
	if len(out) != 2 {
		t.Errorf("expected sample capped at pool size 2, got %d", len(out))
	}
	if out[0].ID == out[1].ID {
		t.Error("sample returned a duplicate")
	}
}
