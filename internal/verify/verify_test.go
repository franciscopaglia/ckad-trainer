// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package verify

import (
	"encoding/json"
	"testing"

	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

const sampleJSON = `{
  "spec": {
    "capacity": {"storage": "1Gi"},
    "accessModes": ["ReadWriteOnce"],
    "hostPath": {"path": "/mnt/data"},
    "containers": [{
      "name": "app",
      "image": "nginx:1.27",
      "resources": {"requests": {"cpu": "500m", "memory": "1Gi"}}
    }],
    "replicas": 3,
    "volumes": [
      {"name": "data", "persistentVolumeClaim": {"claimName": "data-pvc"}}
    ]
  },
  "status": {"phase": "Running"}
}`

func obj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(sampleJSON), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func boolPtr(b bool) *bool { return &b }

func check(t *testing.T, a scenario.Assert, wantPass bool) {
	t.Helper()
	res := Evaluate(obj(t), []scenario.Assert{a})
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if res[0].Pass != wantPass {
		t.Errorf("path %q matcher %+v: pass=%v want %v (got=%q want=%q msg=%q)",
			a.Path, a, res[0].Pass, wantPass, res[0].Got, res[0].Want, res[0].Msg)
	}
}

func TestEquals(t *testing.T) {
	check(t, scenario.Assert{Path: "{.status.phase}", Equals: "Running"}, true)
	check(t, scenario.Assert{Path: "{.status.phase}", Equals: "Pending"}, false)
	check(t, scenario.Assert{Path: "{.spec.capacity.storage}", Equals: "1Gi"}, true)
}

func TestNotEquals(t *testing.T) {
	check(t, scenario.Assert{Path: "{.status.phase}", NotEquals: "Pending"}, true)
	check(t, scenario.Assert{Path: "{.status.phase}", NotEquals: "Running"}, false)
}

func TestContains(t *testing.T) {
	check(t, scenario.Assert{Path: "{.spec.accessModes[*]}", Contains: "ReadWriteOnce"}, true)
	check(t, scenario.Assert{Path: "{.spec.accessModes[*]}", Contains: "ReadWriteMany"}, false)
}

func TestNotContains(t *testing.T) {
	check(t, scenario.Assert{Path: "{.spec.accessModes[*]}", NotContains: "ReadWriteMany"}, true)
	check(t, scenario.Assert{Path: "{.spec.accessModes[*]}", NotContains: "ReadWriteOnce"}, false)
}

func TestIn(t *testing.T) {
	check(t, scenario.Assert{Path: "{.status.phase}", In: []string{"Running", "Succeeded"}}, true)
	check(t, scenario.Assert{Path: "{.status.phase}", In: []string{"Pending", "Failed"}}, false)
}

func TestMatches(t *testing.T) {
	check(t, scenario.Assert{Path: "{.spec.containers[0].image}", Matches: "^nginx:"}, true)
	check(t, scenario.Assert{Path: "{.spec.containers[0].image}", Matches: "^busybox"}, false)
}

func TestExistsWithFilter(t *testing.T) {
	p := `{.spec.volumes[?(@.persistentVolumeClaim.claimName=="data-pvc")].name}`
	check(t, scenario.Assert{Path: p, Exists: boolPtr(true)}, true)
	missing := `{.spec.volumes[?(@.persistentVolumeClaim.claimName=="nope")].name}`
	check(t, scenario.Assert{Path: missing, Exists: boolPtr(true)}, false)
	check(t, scenario.Assert{Path: missing, Exists: boolPtr(false)}, true)
}

func TestEqualsQuantityEquivalence(t *testing.T) {
	cpu := "{.spec.containers[0].resources.requests.cpu}"        // stored as "500m"
	check(t, scenario.Assert{Path: cpu, Equals: "500m"}, true)   // exact string
	check(t, scenario.Assert{Path: cpu, Equals: "0.5"}, true)    // equivalent quantity
	check(t, scenario.Assert{Path: cpu, Equals: "0.6"}, false)   // different quantity
	mem := "{.spec.containers[0].resources.requests.memory}"     // stored as "1Gi"
	check(t, scenario.Assert{Path: mem, Equals: "1024Mi"}, true) // 1Gi == 1024Mi
	check(t, scenario.Assert{Path: mem, Equals: "1G"}, false)    // 1Gi != 1G (1000^3)
}

func TestNotEqualsQuantity(t *testing.T) {
	cpu := "{.spec.containers[0].resources.requests.cpu}"
	check(t, scenario.Assert{Path: cpu, NotEquals: "0.5"}, false) // equal -> not_equals fails
	check(t, scenario.Assert{Path: cpu, NotEquals: "1"}, true)    // different -> passes
}

func TestEqualsStaysStrictForNonQuantity(t *testing.T) {
	// A name field must not get coerced; "Running" vs "running" stays unequal.
	check(t, scenario.Assert{Path: "{.status.phase}", Equals: "running"}, false)
}

func TestGteLteQuantity(t *testing.T) {
	check(t, scenario.Assert{Path: "{.spec.capacity.storage}", Gte: "500Mi"}, true)
	check(t, scenario.Assert{Path: "{.spec.capacity.storage}", Gte: "2Gi"}, false)
	check(t, scenario.Assert{Path: "{.spec.capacity.storage}", Lte: "2Gi"}, true)
}

func TestGteNumeric(t *testing.T) {
	check(t, scenario.Assert{Path: "{.spec.replicas}", Gte: "2"}, true)
	check(t, scenario.Assert{Path: "{.spec.replicas}", Lte: "2"}, false)
}

func TestMissingObjectFails(t *testing.T) {
	res := Evaluate(nil, []scenario.Assert{{Path: "{.status.phase}", Equals: "Running"}})
	if res[0].Pass {
		t.Error("expected fail for missing object")
	}
	if res[0].Got != "<missing>" {
		t.Errorf("got %q, want <missing>", res[0].Got)
	}
}

func TestReportPassed(t *testing.T) {
	r := Report{Results: []Result{{Pass: true}, {Pass: true}}}
	if !r.Passed() {
		t.Error("expected Passed true")
	}
	r.Results[1].Pass = false
	if r.Passed() {
		t.Error("expected Passed false")
	}
}
