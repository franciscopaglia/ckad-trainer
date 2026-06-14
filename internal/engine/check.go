// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/franciscopaglia/ckad-trainer/internal/cluster"
	"github.com/franciscopaglia/ckad-trainer/internal/config"
	"github.com/franciscopaglia/ckad-trainer/internal/kubectl"
	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
	"github.com/franciscopaglia/ckad-trainer/internal/verify"
)

// CheckResult holds the verification of one object (or command drill).
type CheckResult struct {
	Kind    string
	Name    string
	Found   bool
	Results []verify.Result
}

// Passed reports whether every assertion in this check passed.
func (c CheckResult) Passed() bool { return verify.AllPass(c.Results) }

// CheckReport is the full per-check verification of a scenario attempt.
type CheckReport struct {
	Checks []CheckResult
}

// Passed reports whether the whole attempt passed.
func (cr CheckReport) Passed() bool {
	for _, c := range cr.Checks {
		if !c.Passed() {
			return false
		}
	}
	return true
}

// Check verifies the user's work against the scenario's (variant-resolved)
// assertions. It is read-only. Assertions with `wait` are polled until they pass
// or the wait elapses.
func Check(cfg *config.Config, s scenario.Scenario, inst *Instance) (CheckReport, error) {
	return checkWith(cfg, s, inst, true)
}

// CheckQuick is Check without polling: a single snapshot, ignoring `wait`. Used
// for exam status/grade where waiting per task would be too slow.
func CheckQuick(cfg *config.Config, s scenario.Scenario, inst *Instance) (CheckReport, error) {
	return checkWith(cfg, s, inst, false)
}

func checkWith(cfg *config.Config, s scenario.Scenario, inst *Instance, wait bool) (CheckReport, error) {
	kc := kubectl.New(cfg.Cluster.Kubectl, cfg.Cluster.Context)
	data := inst.data()
	var report CheckReport

	for _, raw := range resolveChecks(s, inst) {
		c, err := renderCheck(raw, data)
		if err != nil {
			return report, err
		}

		switch {
		case c.CommandOutput != nil:
			report.Checks = append(report.Checks, evalCommandOutput(s, c, data))
		case c.Script != "":
			report.Checks = append(report.Checks, evalScript(inst, c))
		default:
			report.Checks = append(report.Checks, evalObject(kc, inst, c, wait))
		}
	}
	return report, nil
}

// evalObject fetches the object (cluster- or namespace-scoped) and evaluates its
// asserts. When wait is true it polls while any assert declares a `wait`.
func evalObject(kc *kubectl.Client, inst *Instance, c scenario.Check, wait bool) CheckResult {
	ns := inst.Namespace
	if c.ClusterScoped {
		ns = ""
	}
	deadline := time.Now()
	if wait {
		deadline = deadline.Add(maxWait(c.Assert))
	}
	var (
		obj     map[string]any
		results []verify.Result
		found   bool
	)
	for {
		obj, _ = kc.GetJSON(c.Kind, c.Name, ns) // missing object -> nil, treated as fail
		found = obj != nil
		results = verify.Evaluate(obj, c.Assert)
		// Stop when all pass, when the object is absent (it won't appear by
		// polling), or once the wait window is exhausted.
		if verify.AllPass(results) || !found || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return CheckResult{Kind: c.Kind, Name: c.Name, Found: found, Results: results}
}

// evalCommandOutput runs a command drill (§9c). When EqualsSolution is set it
// compares normalized stdout to the first solution command's output; otherwise it
// passes if the command runs successfully.
func evalCommandOutput(s scenario.Scenario, c scenario.Check, data map[string]any) CheckResult {
	co := c.CommandOutput
	res := evalCommand(co, s, data)
	return CheckResult{Kind: "command", Name: co.Run, Found: res.Got != "<error>", Results: []verify.Result{res}}
}

func evalCommand(co *scenario.CmdOut, s scenario.Scenario, data map[string]any) verify.Result {
	res := verify.Result{Path: co.Run}
	got, err := sh(co.Run)
	if err != nil {
		res.Got, res.Msg = "<error>", err.Error()
		return res
	}
	res.Got = normalize(got, co.Normalize)

	if !co.EqualsSolution || len(s.Solution.Commands) == 0 {
		res.Pass = true // ran successfully; nothing to compare against
		return res
	}
	solCmd, err := render(s.Solution.Commands[0], data)
	if err != nil {
		res.Msg = err.Error()
		return res
	}
	want, err := sh(solCmd)
	if err != nil {
		res.Msg = "solution command failed: " + err.Error()
		return res
	}
	res.Want = normalize(want, co.Normalize)
	res.Pass = res.Got == res.Want
	return res
}

// evalScript runs a check script (exit 0 = pass; stdout = message).
func evalScript(inst *Instance, c scenario.Check) CheckResult {
	cmd := exec.Command("sh", c.Script)
	cmd.Env = append(cmd.Environ(), "NS="+inst.Namespace)
	out, err := cmd.CombinedOutput()
	res := verify.Result{Path: c.Script, Pass: err == nil, Msg: strings.TrimSpace(string(out))}
	return CheckResult{Kind: "script", Name: c.Script, Found: true, Results: []verify.Result{res}}
}

// Solution renders the reference answer for the instance's draw.
func Solution(s scenario.Scenario, inst *Instance) (string, error) {
	data := inst.data()
	var b strings.Builder
	if s.Solution.Description != "" {
		desc, err := render(s.Solution.Description, data)
		if err != nil {
			return "", err
		}
		b.WriteString(strings.TrimRight(desc, "\n"))
		b.WriteString("\n")
	}
	if len(s.Solution.Commands) > 0 {
		b.WriteString("\ncommands:\n")
		for _, cmd := range s.Solution.Commands {
			r, err := render(cmd, data)
			if err != nil {
				return "", err
			}
			b.WriteString("  " + r + "\n")
		}
	}
	for _, m := range s.Solution.Manifests {
		r, err := render(m, data)
		if err != nil {
			return "", err
		}
		b.WriteString("\n---\n")
		b.WriteString(strings.TrimRight(r, "\n"))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// ApplySolution applies the scenario's reference solution (rendered manifests
// and commands) to the cluster. It is mutating, so it passes the safety guard.
// Used by the cluster smoke tests to prove every scenario is solvable.
func ApplySolution(cfg *config.Config, s scenario.Scenario, inst *Instance) error {
	kc := kubectl.New(cfg.Cluster.Kubectl, cfg.Cluster.Context)
	if err := cluster.Guard(cfg, kc); err != nil {
		return err
	}
	data := inst.data()
	for _, m := range s.Solution.Manifests {
		r, err := render(m, data)
		if err != nil {
			return err
		}
		if err := kc.Apply(r); err != nil {
			return err
		}
	}
	for _, c := range s.Solution.Commands {
		r, err := render(c, data)
		if err != nil {
			return err
		}
		line := strings.TrimSpace(r)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip blank lines and comment-only "commands"
		}
		args := strings.Fields(line)
		if args[0] == "kubectl" {
			args = args[1:]
		}
		if _, err := kc.Raw(args...); err != nil {
			return fmt.Errorf("solution command %q: %w", line, err)
		}
	}
	return nil
}

// --- helpers ---

// resolveChecks returns the verify checks for the chosen variant, or the
// top-level checks when there are no variants.
func resolveChecks(s scenario.Scenario, inst *Instance) []scenario.Check {
	if v, ok := findVariant(s, inst.Variant); ok {
		return v.Verify
	}
	return s.Verify
}

// renderCheck returns a copy of the check with its templated fields rendered
// against the instance data. It must NOT mutate the input: scenarios are loaded
// once and reused across draws, and the Assert/In slices share backing arrays
// with the original, so we copy them rather than writing in place.
func renderCheck(c scenario.Check, data map[string]any) (scenario.Check, error) {
	var err error
	render2 := func(s string) string {
		if err != nil || s == "" {
			return s
		}
		var r string
		r, err = render(s, data)
		return r
	}

	out := c // copy the header (Kind, Name, ClusterScoped, Script, ...)
	out.Kind = render2(c.Kind)
	out.Name = render2(c.Name)

	out.Assert = make([]scenario.Assert, len(c.Assert))
	for i, a := range c.Assert { // a is a copy of each element
		a.Path = render2(a.Path)
		a.Equals = render2(a.Equals)
		a.NotEquals = render2(a.NotEquals)
		a.Contains = render2(a.Contains)
		a.NotContains = render2(a.NotContains)
		a.Matches = render2(a.Matches)
		a.Gte = render2(a.Gte)
		a.Lte = render2(a.Lte)
		if len(a.In) > 0 {
			in := make([]string, len(a.In))
			for j, v := range a.In {
				in[j] = render2(v)
			}
			a.In = in
		}
		out.Assert[i] = a
	}

	if c.CommandOutput != nil {
		co := *c.CommandOutput
		co.Run = render2(co.Run)
		out.CommandOutput = &co
	}
	return out, err
}

// maxWait returns the longest `wait` declared among the asserts (0 if none).
func maxWait(asserts []scenario.Assert) time.Duration {
	var longest time.Duration
	for _, a := range asserts {
		if a.Wait == "" {
			continue
		}
		if d, err := time.ParseDuration(a.Wait); err == nil && d > longest {
			longest = d
		}
	}
	return longest
}

// sh runs a shell command and returns trimmed stdout.
func sh(command string) (string, error) {
	out, err := exec.Command("sh", "-c", command).Output()
	return strings.TrimSpace(string(out)), err
}

// normalize trims/collapses whitespace, optionally sorting tokens so that
// order-insensitive command output (e.g. a list of names) compares equal.
func normalize(s, mode string) string {
	fields := strings.Fields(s)
	if mode == "sort_whitespace" {
		sort.Strings(fields)
	}
	return strings.Join(fields, " ")
}
