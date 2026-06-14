// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package catalog

import (
	"testing"

	"github.com/franciscopaglia/ckad-trainer/internal/engine"
	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

// TestCatalogLoadsAndRenders parses + validates the whole embedded catalog and
// renders every scenario for 3 fixed seeds. Cluster-free; runs in `go test ./...`.
func TestCatalogLoadsAndRenders(t *testing.T) {
	scenarios, err := scenario.LoadAll(FS())
	if err != nil {
		t.Fatalf("loading embedded catalog: %v", err)
	}
	if len(scenarios) == 0 {
		t.Fatal("embedded catalog is empty")
	}
	for _, s := range scenarios {
		for _, seed := range []int64{1, 2, 3} {
			if err := engine.CheckDraw(s, seed); err != nil {
				t.Errorf("%s (seed %d): %v", s.ID, seed, err)
			}
		}
	}
}
