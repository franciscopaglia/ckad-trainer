// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package exam runs timed, weighted exam sessions: it samples scenarios across
// the official CKAD domains, starts them all, tracks a deadline, and grades the
// whole set at the end with a per-domain breakdown.
package exam

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/franciscopaglia/ckad-trainer/internal/config"
	"github.com/franciscopaglia/ckad-trainer/internal/engine"
	"github.com/franciscopaglia/ckad-trainer/internal/paths"
	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

// Official 2026 CKAD domains and their exam weights. Fine-grained scenario domain
// slugs map onto these for weighted sampling and scoring.
var officialDomain = map[string]string{
	"configuration":       "App Config & Security",
	"security":            "App Config & Security",
	"state-persistence":   "App Config & Security",
	"multi-container":     "App Design & Build",
	"core-concepts":       "App Design & Build",
	"pod-design":          "App Deployment",
	"services-networking": "Services & Networking",
	"observability":       "Observability & Maintenance",
}

var domainWeight = map[string]int{
	"App Config & Security":       25,
	"App Design & Build":          20,
	"App Deployment":              20,
	"Services & Networking":       20,
	"Observability & Maintenance": 15,
}

func officialOf(slug string) string {
	if d, ok := officialDomain[slug]; ok {
		return d
	}
	return "Other"
}

func weightOf(slug string) int {
	if w, ok := domainWeight[officialOf(slug)]; ok {
		return w
	}
	return 10
}

// sessionFile is the persisted exam session, kept alongside the scenario state
// files in the per-user state directory.
func sessionFile() string { return filepath.Join(paths.StateDir(), "exam.json") }

// Task is one exam item (a started scenario).
type Task struct {
	ScenarioID string `json:"scenario_id"`
	Domain     string `json:"domain"` // fine-grained slug
}

// Session is the persisted exam state.
type Session struct {
	StartedAt time.Time `json:"started_at"`
	Minutes   int       `json:"minutes"`
	Tasks     []Task    `json:"tasks"`
}

// Deadline is when time runs out.
func (s *Session) Deadline() time.Time {
	return s.StartedAt.Add(time.Duration(s.Minutes) * time.Minute)
}

// Remaining is the time left (negative once past the deadline).
func (s *Session) Remaining() time.Duration {
	return time.Until(s.Deadline())
}

// Start samples count scenarios weighted by domain, starts each, and persists the
// session. A non-zero seed makes the selection + draws reproducible.
func Start(cfg *config.Config, scenarios []scenario.Scenario, count, minutes int, seed int64) (*Session, error) {
	if _, err := os.Stat(sessionFile()); err == nil {
		return nil, errors.New("an exam is already in progress (run `exam grade` or `exam abort` first)")
	}
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	// Pool: practice/exam scenarios not already active (flashcards excluded).
	var pool []scenario.Scenario
	for _, s := range scenarios {
		if s.Mode == scenario.ModeFlashcard || engine.HasState(s.ID) {
			continue
		}
		pool = append(pool, s)
	}
	picked := sample(pool, count, rng)
	if len(picked) == 0 {
		return nil, errors.New("no scenarios available to sample for an exam")
	}

	var started []*engine.Instance
	var tasks []Task
	for _, s := range picked {
		inst, err := engine.Start(cfg, s, rng.Int63())
		if err != nil {
			for _, prev := range started { // roll back on failure
				_ = engine.Cleanup(cfg, prev)
			}
			return nil, fmt.Errorf("starting %q: %w", s.ID, err)
		}
		started = append(started, inst)
		tasks = append(tasks, Task{ScenarioID: s.ID, Domain: s.Domain})
	}

	sess := &Session{StartedAt: time.Now().UTC(), Minutes: minutes, Tasks: tasks}
	if err := save(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// sample picks count scenarios from pool, weighted by domain, without replacement.
func sample(pool []scenario.Scenario, count int, rng *rand.Rand) []scenario.Scenario {
	remaining := append([]scenario.Scenario(nil), pool...)
	var out []scenario.Scenario
	for len(out) < count && len(remaining) > 0 {
		total := 0
		for _, s := range remaining {
			total += weightOf(s.Domain)
		}
		r := rng.Intn(total)
		idx := len(remaining) - 1
		for i, s := range remaining {
			if r < weightOf(s.Domain) {
				idx = i
				break
			}
			r -= weightOf(s.Domain)
		}
		out = append(out, remaining[idx])
		remaining = append(remaining[:idx], remaining[idx+1:]...)
	}
	return out
}

// InProgress reports whether an exam session is active.
func InProgress() bool {
	_, err := os.Stat(sessionFile())
	return err == nil
}

// Clear removes the exam session file (no-op if there is none). The task
// resources are ordinary scenario instances, cleaned up separately.
func Clear() error {
	if err := os.Remove(sessionFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Load reads the active session, or an error if there is none.
func Load() (*Session, error) {
	raw, err := os.ReadFile(sessionFile())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("no exam in progress (start one with `exam start`)")
		}
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("reading exam session: %w", err)
	}
	return &s, nil
}

// DomainScore is a per-domain tally.
type DomainScore struct {
	Domain string
	Passed int
	Total  int
}

// TaskResult is one task's outcome.
type TaskResult struct {
	ScenarioID string
	Domain     string
	Passed     bool
}

// GradeReport is the scored result of an exam.
type GradeReport struct {
	Passed        int
	Total         int
	WeightedScore float64 // 0..100, weighted by domain
	Domains       []DomainScore
	Tasks         []TaskResult
}

// Grade checks every task (snapshot, no waiting), scores the session, cleans up
// all task resources, and ends the session.
func Grade(cfg *config.Config, scenarios []scenario.Scenario) (GradeReport, error) {
	sess, err := Load()
	if err != nil {
		return GradeReport{}, err
	}
	var rep GradeReport
	byDomain := map[string]*DomainScore{}

	for _, t := range sess.Tasks {
		passed := false
		if s, ferr := scenario.Find(scenarios, t.ScenarioID); ferr == nil {
			if inst, lerr := engine.LoadInstance(t.ScenarioID); lerr == nil {
				// Grade with waiting: settling objects (Bound/ready/endpoints) get
				// their wait; undone tasks (missing object) fail immediately.
				if report, cerr := engine.Check(cfg, s, inst); cerr == nil {
					passed = report.Passed()
				}
				_ = engine.Cleanup(cfg, inst)
			}
		}
		od := officialOf(t.Domain)
		ds := byDomain[od]
		if ds == nil {
			ds = &DomainScore{Domain: od}
			byDomain[od] = ds
		}
		ds.Total++
		rep.Total++
		if passed {
			ds.Passed++
			rep.Passed++
		}
		rep.Tasks = append(rep.Tasks, TaskResult{ScenarioID: t.ScenarioID, Domain: od, Passed: passed})
	}

	rep.Domains, rep.WeightedScore = summarize(byDomain)
	_ = os.Remove(sessionFile())
	return rep, nil
}

// summarize sorts the domain scores and computes the weighted score: the average
// pass rate across the domains present, weighted by each domain's exam weight.
func summarize(byDomain map[string]*DomainScore) ([]DomainScore, float64) {
	var domains []DomainScore
	var weightedSum, weightTotal float64
	for name, ds := range byDomain {
		domains = append(domains, *ds)
		w := float64(domainWeight[name])
		if w == 0 {
			w = 10
		}
		weightedSum += w * (float64(ds.Passed) / float64(ds.Total))
		weightTotal += w
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i].Domain < domains[j].Domain })
	score := 0.0
	if weightTotal > 0 {
		score = 100 * weightedSum / weightTotal
	}
	return domains, score
}

// Abort cleans up all task resources and ends the session without scoring.
func Abort(cfg *config.Config) error {
	sess, err := Load()
	if err != nil {
		return err
	}
	for _, t := range sess.Tasks {
		if inst, lerr := engine.LoadInstance(t.ScenarioID); lerr == nil {
			_ = engine.Cleanup(cfg, inst)
		}
	}
	return os.Remove(sessionFile())
}

func save(s *Session) error {
	if err := os.MkdirAll(filepath.Dir(sessionFile()), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionFile(), raw, 0o644)
}
