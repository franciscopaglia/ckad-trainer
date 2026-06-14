// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package scenario loads and validates scenario definition files.
//
// A scenario is one YAML file (data, never code). The engine is generic and is
// driven entirely by these files. See PLAN.md §5 and Appendix A.
package scenario

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// dns1123 matches a valid lowercase DNS label (used for IDs and namespaces).
var dns1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Modes a scenario may declare.
const (
	ModePractice  = "practice"
	ModeExam      = "exam"
	ModeFlashcard = "flashcard"
)

// Domains is the set of allowed domain slugs. These finer-grained slugs are nicer
// for `list --domain`; exam mode (Phase 5) maps each to an official 2026 domain
// and weight for sampling.
var Domains = map[string]bool{
	"core-concepts":       true,
	"multi-container":     true,
	"pod-design":          true,
	"configuration":       true,
	"security":            true,
	"observability":       true,
	"services-networking": true,
	"state-persistence":   true,
}

// Param declares a randomizable variable (resolved at start; see Phase 4).
type Param struct {
	Pick    []string `yaml:"pick,omitempty"`
	Choice  []string `yaml:"choice,omitempty"`
	Range   *Range   `yaml:"range,omitempty"`
	Pattern string   `yaml:"pattern,omitempty"`
}

// Range is an integer draw.
type Range struct {
	Min  int `yaml:"min"`
	Max  int `yaml:"max"`
	Step int `yaml:"step,omitempty"`
}

// Assert is one verification: a path plus exactly one matcher.
type Assert struct {
	Path        string   `yaml:"path"`
	Equals      string   `yaml:"equals,omitempty"`
	NotEquals   string   `yaml:"not_equals,omitempty"`
	Contains    string   `yaml:"contains,omitempty"`
	NotContains string   `yaml:"not_contains,omitempty"`
	In          []string `yaml:"in,omitempty"`
	Exists      *bool    `yaml:"exists,omitempty"`
	Matches     string   `yaml:"matches,omitempty"`
	Gte         string   `yaml:"gte,omitempty"`
	Lte         string   `yaml:"lte,omitempty"`
	Wait        string   `yaml:"wait,omitempty"`
}

// matcherCount returns how many matchers are set (must be exactly 1).
func (a Assert) matcherCount() int {
	n := 0
	for _, s := range []string{a.Equals, a.NotEquals, a.Contains, a.NotContains, a.Matches, a.Gte, a.Lte} {
		if s != "" {
			n++
		}
	}
	if len(a.In) > 0 {
		n++
	}
	if a.Exists != nil {
		n++
	}
	return n
}

// CmdOut is the command-drill verify mode (§9c).
type CmdOut struct {
	Run            string `yaml:"run"`
	Normalize      string `yaml:"normalize,omitempty"`
	EqualsSolution bool   `yaml:"equals_solution,omitempty"`
}

// Check verifies one object (or a command's output).
type Check struct {
	Kind          string   `yaml:"kind,omitempty"`
	Name          string   `yaml:"name,omitempty"`
	ClusterScoped bool     `yaml:"cluster_scoped,omitempty"`
	Assert        []Assert `yaml:"assert,omitempty"`
	Script        string   `yaml:"script,omitempty"`
	CommandOutput *CmdOut  `yaml:"command_output,omitempty"`
}

// Variant is one alternative form of a scenario (drawn at start).
type Variant struct {
	Name   string           `yaml:"name"`
	Weight int              `yaml:"weight,omitempty"`
	Prompt string           `yaml:"prompt,omitempty"`
	Verify []Check          `yaml:"verify,omitempty"`
	Params map[string]Param `yaml:"params,omitempty"`
}

// Setup is pre-created state before the user starts.
type Setup struct {
	Manifests []string `yaml:"manifests,omitempty"`
	Commands  []string `yaml:"commands,omitempty"`
}

// Solution is the reference answer.
type Solution struct {
	Description string   `yaml:"description,omitempty"`
	Commands    []string `yaml:"commands,omitempty"`
	Manifests   []string `yaml:"manifests,omitempty"`
}

// ObjRef names a cluster-scoped object to clean up.
type ObjRef struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

// Cleanup lists extra cluster-scoped objects to delete (namespace is automatic).
type Cleanup struct {
	ClusterScoped []ObjRef `yaml:"cluster_scoped,omitempty"`
}

// Scenario is one task definition.
type Scenario struct {
	ID               string           `yaml:"id"`
	Title            string           `yaml:"title"`
	Mode             string           `yaml:"mode"`
	Domain           string           `yaml:"domain"`
	Weight           int              `yaml:"weight,omitempty"`
	EstimatedMinutes int              `yaml:"estimated_minutes,omitempty"`
	Randomize        bool             `yaml:"randomize,omitempty"`
	References       []string         `yaml:"references,omitempty"`
	Params           map[string]Param `yaml:"params,omitempty"`
	Variants         []Variant        `yaml:"variants,omitempty"`
	Setup            Setup            `yaml:"setup,omitempty"`
	Prompt           string           `yaml:"prompt,omitempty"`
	Hints            []string         `yaml:"hints,omitempty"`
	Verify           []Check          `yaml:"verify,omitempty"`
	Solution         Solution         `yaml:"solution,omitempty"`
	Cleanup          Cleanup          `yaml:"cleanup,omitempty"`

	// SourceFile is the path the scenario was loaded from (for error messages).
	SourceFile string `yaml:"-"`
}

// LoadAll walks fsys for *.yaml files, parses them, and validates each. It also
// enforces ID uniqueness across the whole catalog.
func LoadAll(fsys fs.FS) ([]Scenario, error) {
	var scenarios []Scenario
	seen := map[string]string{} // id -> file
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		var s Scenario
		if err := yaml.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		s.SourceFile = path
		s.normalize()
		if err := s.Validate(); err != nil {
			return err
		}
		if prev, dup := seen[s.ID]; dup {
			return fmt.Errorf("duplicate scenario id %q in %s and %s", s.ID, prev, path)
		}
		seen[s.ID] = path
		scenarios = append(scenarios, s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return scenarios, nil
}

// Find returns the scenario with the given id, or an error.
func Find(scenarios []Scenario, id string) (Scenario, error) {
	for _, s := range scenarios {
		if s.ID == id {
			return s, nil
		}
	}
	return Scenario{}, fmt.Errorf("no scenario with id %q", id)
}

// normalize fills convenience defaults before validation.
func (s *Scenario) normalize() {
	if s.Mode == "" {
		s.Mode = ModePractice
	}
}

// Validate enforces the rules in PLAN.md §4d. Randomization-specific render
// checks across multiple seeds are added in Phase 4; here we parse-check
// templates only.
func (s Scenario) Validate() error {
	where := s.SourceFile
	if where == "" {
		where = s.ID
	}
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%s: %s", where, fmt.Sprintf(format, a...))
	}

	if s.ID == "" {
		return bad("id is required")
	}
	if !dns1123.MatchString(s.ID) {
		return bad("id %q is not DNS-1123 (lowercase alphanumerics and dashes)", s.ID)
	}
	switch s.Mode {
	case ModePractice, ModeExam, ModeFlashcard:
	default:
		return bad("mode %q must be practice, exam, or flashcard", s.Mode)
	}
	if !Domains[s.Domain] {
		return bad("domain %q is not a known domain slug", s.Domain)
	}

	hasVariants := len(s.Variants) > 0
	if hasVariants && (s.Prompt != "" || len(s.Verify) > 0) {
		return bad("use either variants OR a top-level prompt+verify, not both")
	}
	if !hasVariants && s.Mode != ModeFlashcard {
		if s.Prompt == "" {
			return bad("prompt is required (no variants given)")
		}
	}

	// Collect all checks (top-level and per-variant) and validate them.
	checkSets := [][]Check{s.Verify}
	for _, v := range s.Variants {
		if v.Name == "" {
			return bad("a variant is missing its name")
		}
		checkSets = append(checkSets, v.Verify)
	}
	for _, checks := range checkSets {
		for _, c := range checks {
			if err := c.validate(bad); err != nil {
				return err
			}
		}
	}

	// cluster_scoped checks must be cleaned up, or they leak.
	for _, checks := range checkSets {
		for _, c := range checks {
			if c.ClusterScoped && !s.cleansUp(c.Kind, c.Name) {
				return bad("cluster_scoped check %s/%s is not listed in cleanup.cluster_scoped", c.Kind, c.Name)
			}
		}
	}

	// Templates must at least parse.
	for _, tmpl := range s.templatedStrings() {
		if _, err := template.New("t").Parse(tmpl); err != nil {
			return bad("template parse error: %v", err)
		}
	}
	return nil
}

func (c Check) validate(bad func(string, ...any) error) error {
	forms := 0
	if len(c.Assert) > 0 {
		forms++
	}
	if c.Script != "" {
		forms++
	}
	if c.CommandOutput != nil {
		forms++
	}
	if forms == 0 {
		return bad("check %s/%s has no assert, script, or command_output", c.Kind, c.Name)
	}
	for _, a := range c.Assert {
		if a.Path == "" {
			return bad("an assert on %s/%s has no path", c.Kind, c.Name)
		}
		if n := a.matcherCount(); n != 1 {
			return bad("assert on path %q must set exactly one matcher, found %d", a.Path, n)
		}
	}
	return nil
}

// cleansUp reports whether kind/name is listed in cleanup.cluster_scoped.
func (s Scenario) cleansUp(kind, name string) bool {
	for _, ref := range s.Cleanup.ClusterScoped {
		if strings.EqualFold(ref.Kind, kind) && ref.Name == name {
			return true
		}
	}
	return false
}

// templatedStrings returns every field that is rendered as a template.
func (s Scenario) templatedStrings() []string {
	var out []string
	add := func(ss ...string) { out = append(out, ss...) }
	add(s.Prompt)
	add(s.Setup.Manifests...)
	add(s.Setup.Commands...)
	add(s.Solution.Manifests...)
	add(s.Solution.Commands...)
	add(s.Solution.Description)
	for _, v := range s.Variants {
		add(v.Prompt)
	}
	for _, ref := range s.Cleanup.ClusterScoped {
		add(ref.Name)
	}
	return out
}
