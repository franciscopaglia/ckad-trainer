// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build cluster

// Cluster smoke test: for every scenario and 3 seeds, start it, apply its own
// reference solution, and assert the check PASSes; then clean up. This proves
// each scenario is solvable across its random draws.
//
// Run with:  go test -tags=cluster .
// It mutates the cluster named by config.yaml (guarded by safety.require_context).
package catalog

import (
	"testing"

	"github.com/franciscopaglia/ckad-trainer/internal/config"
	"github.com/franciscopaglia/ckad-trainer/internal/engine"
	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

func TestSmokeSolutions(t *testing.T) {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		t.Fatalf("loading config.yaml: %v", err)
	}
	scenarios, err := scenario.LoadAll(FS())
	if err != nil {
		t.Fatalf("loading catalog: %v", err)
	}

	for _, s := range scenarios {
		s := s
		if s.Mode == scenario.ModeFlashcard {
			continue // flashcards are text-recall drills, not object-verified
		}
		t.Run(s.ID, func(t *testing.T) {
			for _, seed := range []int64{1, 2, 3} {
				cleanLeftover(t, cfg, s.ID)

				inst, err := engine.Start(cfg, s, seed)
				if err != nil {
					t.Fatalf("seed %d: start: %v", seed, err)
				}
				if err := engine.ApplySolution(cfg, s, inst); err != nil {
					engine.Cleanup(cfg, inst)
					t.Fatalf("seed %d (variant %q): apply solution: %v", seed, inst.Variant, err)
				}
				report, err := engine.Check(cfg, s, inst)
				if err != nil {
					engine.Cleanup(cfg, inst)
					t.Fatalf("seed %d: check: %v", seed, err)
				}
				if !report.Passed() {
					for _, c := range report.Checks {
						for _, r := range c.Results {
							if !r.Pass {
								t.Errorf("seed %d variant %q: %s/%s %s want=%q got=%q %s",
									seed, inst.Variant, c.Kind, c.Name, r.Path, r.Want, r.Got, r.Msg)
							}
						}
					}
				}
				if err := engine.Cleanup(cfg, inst); err != nil {
					t.Fatalf("seed %d: cleanup: %v", seed, err)
				}
			}
		})
	}
}

// cleanLeftover removes any state from a previously aborted run so Start does not
// refuse with ErrAlreadyStarted.
func cleanLeftover(t *testing.T, cfg *config.Config, id string) {
	t.Helper()
	if !engine.HasState(id) {
		return
	}
	inst, err := engine.LoadInstance(id)
	if err != nil {
		return
	}
	_ = engine.Cleanup(cfg, inst)
}
