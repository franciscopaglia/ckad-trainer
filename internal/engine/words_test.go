// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"reflect"
	"testing"

	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

func TestSplitWords(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"kubectl get pods", []string{"kubectl", "get", "pods"}},
		{"  spaced   out\targs ", []string{"spaced", "out", "args"}},
		{`create cm x --from-literal=key="a b"`, []string{"create", "cm", "x", "--from-literal=key=a b"}},
		{`annotate pod x note='hello world'`, []string{"annotate", "pod", "x", "note=hello world"}},
		{`patch svc web -p '{"spec":{"selector":{"app":"web"}}}'`,
			[]string{"patch", "svc", "web", "-p", `{"spec":{"selector":{"app":"web"}}}`}},
		{`echo "esc \" and \\ done"`, []string{"echo", `esc " and \ done`}},
		{`back\ slash`, []string{"back slash"}},
		{"", nil},
	}
	for _, c := range cases {
		got, err := splitWords(c.in)
		if err != nil {
			t.Errorf("splitWords(%q): unexpected error %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitWords(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}

	for _, bad := range []string{`unbalanced "quote`, `unbalanced 'quote`} {
		if _, err := splitWords(bad); err == nil {
			t.Errorf("splitWords(%q): expected error", bad)
		}
	}
}

func TestKubectlArgs(t *testing.T) {
	args, err := kubectlArgs("kubectl get pods -n test")
	if err != nil || !reflect.DeepEqual(args, []string{"get", "pods", "-n", "test"}) {
		t.Errorf("kubectl prefix not stripped: %v, %v", args, err)
	}
	for _, skip := range []string{"", "   ", "# just a comment"} {
		if args, err := kubectlArgs(skip); err != nil || args != nil {
			t.Errorf("kubectlArgs(%q) = %v, %v; want nil, nil", skip, args, err)
		}
	}
}

// TestCheckDrawRejectsUnparsableCommand: commands run without a shell, so an
// unbalanced quote must fail catalog validation, not on the cluster.
func TestCheckDrawRejectsUnparsableCommand(t *testing.T) {
	s := scenario.Scenario{
		ID: "t", Mode: scenario.ModePractice, Domain: "configuration",
		Prompt: "p",
		Setup:  scenario.Setup{Commands: []string{`kubectl annotate pod x note="unbalanced`}},
	}
	if err := CheckDraw(s, 1); err == nil {
		t.Fatal("expected CheckDraw to reject the unbalanced quote")
	}
}
