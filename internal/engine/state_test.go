// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"testing"
	"time"
)

func TestRecordCheckPersists(t *testing.T) {
	t.Setenv("CKAD_TRAINER_STATE", t.TempDir())
	inst := &Instance{ScenarioID: "demo", Namespace: "ns-demo", StartedAt: time.Now().UTC()}
	if err := writeState(inst); err != nil {
		t.Fatal(err)
	}

	// A freshly started instance has not been checked yet.
	got, err := LoadInstance("demo")
	if err != nil {
		t.Fatal(err)
	}
	if !got.CheckedAt.IsZero() || got.Passed {
		t.Fatalf("fresh instance should be unchecked, got %+v", got)
	}

	// A passing check is persisted.
	if err := RecordCheck(inst, true); err != nil {
		t.Fatal(err)
	}
	got, err = LoadInstance("demo")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Passed || got.CheckedAt.IsZero() {
		t.Errorf("passing check not persisted: %+v", got)
	}

	// A subsequent failing check flips Passed back but keeps CheckedAt set.
	if err := RecordCheck(inst, false); err != nil {
		t.Fatal(err)
	}
	got, err = LoadInstance("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Passed {
		t.Errorf("Passed should be false after a failing check")
	}
	if got.CheckedAt.IsZero() {
		t.Errorf("CheckedAt should remain set after a failing check")
	}
}
