// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package scenario

import "testing"

func base() Scenario {
	return Scenario{
		ID:     "demo-scenario",
		Title:  "Demo",
		Mode:   ModePractice,
		Domain: "configuration",
		Prompt: "do the thing in {{.ns}}",
		Verify: []Check{{
			Kind: "Pod", Name: "app",
			Assert: []Assert{{Path: "{.status.phase}", Equals: "Running"}},
		}},
	}
}

func TestValidateGood(t *testing.T) {
	if err := base().Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateBadID(t *testing.T) {
	s := base()
	s.ID = "Bad_ID"
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for non-DNS-1123 id")
	}
}

func TestValidateBadDomain(t *testing.T) {
	s := base()
	s.Domain = "made-up"
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for unknown domain")
	}
}

func TestValidateBadMode(t *testing.T) {
	s := base()
	s.Mode = "quiz"
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestValidateTwoMatchers(t *testing.T) {
	s := base()
	s.Verify[0].Assert[0] = Assert{Path: "{.x}", Equals: "a", Contains: "b"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for two matchers on one assert")
	}
}

func TestValidateNoMatcher(t *testing.T) {
	s := base()
	s.Verify[0].Assert[0] = Assert{Path: "{.x}"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for zero matchers on one assert")
	}
}

func TestValidateVariantsAndTopLevel(t *testing.T) {
	s := base()
	s.Variants = []Variant{{Name: "v1", Prompt: "p"}}
	// base() already has top-level Prompt+Verify, so this must be rejected.
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for both variants and top-level prompt/verify")
	}
}

func TestValidateClusterScopedMustBeCleaned(t *testing.T) {
	s := base()
	s.Verify[0] = Check{
		Kind: "PersistentVolume", Name: "pv1", ClusterScoped: true,
		Assert: []Assert{{Path: "{.x}", Equals: "y"}},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error: cluster-scoped check not in cleanup")
	}
	s.Cleanup.ClusterScoped = []ObjRef{{Kind: "PersistentVolume", Name: "pv1"}}
	if err := s.Validate(); err != nil {
		t.Fatalf("expected valid once cleanup lists the PV, got %v", err)
	}
}

func TestValidateFlashcardNeedsNoPrompt(t *testing.T) {
	s := Scenario{
		ID: "fc", Mode: ModeFlashcard, Domain: "configuration",
		Verify: []Check{{CommandOutput: &CmdOut{Run: "kubectl get po"}}},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("flashcard should be valid without a prompt, got %v", err)
	}
}
