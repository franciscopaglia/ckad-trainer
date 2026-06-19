// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Command ckad-trainer is a local CKAD practice runner. It sets up scenarios in
// your cluster, checks your work, and cleans up — plus a timed, scored exam mode.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	catalog "github.com/franciscopaglia/ckad-trainer"
	"github.com/franciscopaglia/ckad-trainer/internal/cluster"
	"github.com/franciscopaglia/ckad-trainer/internal/config"
	"github.com/franciscopaglia/ckad-trainer/internal/engine"
	"github.com/franciscopaglia/ckad-trainer/internal/exam"
	"github.com/franciscopaglia/ckad-trainer/internal/kubectl"
	"github.com/franciscopaglia/ckad-trainer/internal/paths"
	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
	"github.com/spf13/cobra"
)

var configPath string

// version is stamped at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:           "ckad-trainer",
		Short:         "Local CKAD practice: scenario setup, checking, and cleanup",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&configPath, "config", "",
		"path to config file (default: ./config.yaml, else $XDG_CONFIG_HOME/ckad-trainer/config.yaml)")

	root.AddCommand(
		initCmd(), doctorCmd(), startCmd(), statusCmd(), checkCmd(), solutionCmd(), solveCmd(), cleanupCmd(),
		resetCmd(), listCmd(), randomCmd(), drillCmd(), examCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// loadScenarios reads the catalog: from disk when cfg.ScenarioDir is set
// (authoring), otherwise from the catalog embedded in the binary.
func loadScenarios(cfg *config.Config) ([]scenario.Scenario, error) {
	if cfg.ScenarioDir != "" {
		return scenario.LoadAll(os.DirFS(cfg.ScenarioDir))
	}
	return scenario.LoadAll(catalog.FS())
}

// loadConfig loads the config file (resolved from --config, ./config.yaml, or the
// per-user XDG path), or — when there is none — falls back to the current kube
// context, so a freshly downloaded binary works with no setup.
func loadConfig() (*config.Config, error) {
	path := paths.ResolveConfig(configPath)
	cfg, err := config.Load(path)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, config.ErrNotFound) {
		return nil, err
	}
	cur, derr := kubectl.New("kubectl", "").CurrentContext()
	if derr != nil || cur == "" {
		return nil, fmt.Errorf("no config file, and no current kube context to fall back on.\n" +
			"Point kubectl at a cluster (`kubectl config use-context <name>`), then run `ckad-trainer init`")
	}
	fmt.Fprintln(os.Stderr, dim(fmt.Sprintf("no config file — using current kube context %q (run `ckad-trainer init` to pin it)", cur)))
	return config.Default(cur), nil
}

func initCmd() *cobra.Command {
	var ctx string
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a config file pinned to your current kube context",
		Long: "Write a config file pinned to your current kube context.\n\n" +
			"By default it writes the per-user config ($XDG_CONFIG_HOME/ckad-trainer/config.yaml),\n" +
			"so an installed binary works from any directory. If a ./config.yaml already exists it\n" +
			"updates that instead; --config picks an explicit path.",
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := paths.ResolveConfig(configPath)
			if _, err := os.Stat(dest); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", dest)
			}
			if ctx == "" {
				cur, err := kubectl.New("kubectl", "").CurrentContext()
				if err != nil || cur == "" {
					return fmt.Errorf("no current kube context; pass --context <name> or run `kubectl config use-context <name>` first")
				}
				ctx = cur
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dest, []byte(config.Template(ctx)), 0o644); err != nil {
				return err
			}
			fmt.Printf("wrote %s pinned to context %q\nnext: ckad-trainer doctor\n", dest, ctx)
			return nil
		},
	}
	cmd.Flags().StringVar(&ctx, "context", "", "kube context to pin (default: your current one)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config")
	return cmd
}

// resolveCatalog loads the config and the whole scenario catalog.
func resolveCatalog() (*config.Config, []scenario.Scenario, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	scenarios, err := loadScenarios(cfg)
	return cfg, scenarios, err
}

// resolveScenario loads the config and finds one scenario by id.
func resolveScenario(id string) (*config.Config, scenario.Scenario, error) {
	cfg, scenarios, err := resolveCatalog()
	if err != nil {
		return nil, scenario.Scenario{}, err
	}
	s, err := scenario.Find(scenarios, id)
	return cfg, s, err
}

// resolveStarted is resolveScenario plus the persisted instance, for commands
// that act on a running attempt (check, solution).
func resolveStarted(id string) (*config.Config, scenario.Scenario, *engine.Instance, error) {
	cfg, s, err := resolveScenario(id)
	if err != nil {
		return nil, scenario.Scenario{}, nil, err
	}
	inst, err := engine.LoadInstance(id)
	return cfg, s, inst, err
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check kubectl, the safety context guard, and cluster reachability",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			results, ok := cluster.Doctor(cfg)
			for _, r := range results {
				mark := red("x")
				if r.OK {
					mark = green("+")
				}
				fmt.Printf("[%s] %-18s %s\n", mark, r.Name, r.Detail)
			}
			fmt.Println()
			if ok {
				fmt.Println(green("doctor: ready"))
				return nil
			}
			return fmt.Errorf("doctor: not ready (see failures above)")
		},
	}
}

func startCmd() *cobra.Command {
	var force bool
	var seed int64
	cmd := &cobra.Command{
		Use:   "start <scenario-id>",
		Short: "Set up a scenario and print the task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			cfg, s, err := resolveScenario(id)
			if err != nil {
				return err
			}

			return startAndPrint(cfg, s, seed, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "restart the scenario if it is already running")
	cmd.Flags().Int64Var(&seed, "seed", 0, "fix the random draw for a reproducible scenario (0 = random)")
	return cmd
}

// startAndPrint sets up a scenario and prints the task. Shared by `start` and
// `random`.
func startAndPrint(cfg *config.Config, s scenario.Scenario, seed int64, force bool) error {
	if engine.HasState(s.ID) {
		if !force {
			return fmt.Errorf("%q is already started — re-check it, `cleanup %s`, or `start --force`", s.ID, s.ID)
		}
		inst, err := engine.LoadInstance(s.ID)
		if err != nil {
			return err
		}
		if err := engine.Cleanup(cfg, inst); err != nil {
			return fmt.Errorf("force cleanup failed: %w", err)
		}
	}
	inst, err := engine.Start(cfg, s, seed)
	if err != nil {
		return err
	}
	prompt, err := engine.RenderPrompt(s, inst)
	if err != nil {
		return err
	}
	fmt.Printf("%s  [%s · %s]\n", bold(s.Title), s.Mode, s.Domain)
	fmt.Printf("namespace: %s  %s\n\n", inst.Namespace, dim(fmt.Sprintf("(seed %d)", inst.Seed)))
	fmt.Println(prompt)
	if len(s.Hints) > 0 {
		fmt.Println("hints:")
		for _, h := range s.Hints {
			fmt.Printf("  - %s\n", h)
		}
	}
	fmt.Printf("\nwhen done:  ckad-trainer check %s\n", s.ID)
	fmt.Printf("give up:    ckad-trainer solution %s   |   ckad-trainer cleanup %s\n", s.ID, s.ID)
	return nil
}

func randomCmd() *cobra.Command {
	var domain string
	var seed int64
	cmd := &cobra.Command{
		Use:   "random",
		Short: "Start a random scenario (optionally from one domain)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, scenarios, err := resolveCatalog()
			if err != nil {
				return err
			}
			var pool []scenario.Scenario
			for _, s := range scenarios {
				if s.Mode == scenario.ModeFlashcard || engine.HasState(s.ID) {
					continue
				}
				if domain != "" && s.Domain != domain {
					continue
				}
				pool = append(pool, s)
			}
			if len(pool) == 0 {
				return fmt.Errorf("no available scenarios to pick from")
			}
			s := pool[rand.Intn(len(pool))]
			return startAndPrint(cfg, s, seed, false)
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "limit to a domain slug")
	cmd.Flags().Int64Var(&seed, "seed", 0, "fix the scenario's draw (0 = random)")
	return cmd
}

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <scenario-id>",
		Short: "Verify your work; prints a per-assertion PASS/FAIL table",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			cfg, s, inst, err := resolveStarted(id)
			if err != nil {
				return err
			}
			report, err := engine.Check(cfg, s, inst)
			if err != nil {
				return err
			}
			printReport(report)
			if report.Passed() {
				fmt.Printf("\n%s — %s\n", green("PASS"), s.Title)
				return nil
			}
			fmt.Printf("\n%s — keep going, then `check %s` again (or `solution %s`)\n", red("FAIL"), id, id)
			return fmt.Errorf("scenario not yet passing")
		},
	}
}

func printReport(report engine.CheckReport) {
	for _, c := range report.Checks {
		status := ""
		if !c.Found && c.Kind != "command" && c.Kind != "script" {
			status = "  (not found)"
		}
		fmt.Printf("%s/%s%s\n", c.Kind, c.Name, status)
		for _, r := range c.Results {
			mark := red("FAIL")
			if r.Pass {
				mark = green("PASS")
			}
			line := fmt.Sprintf("  [%s] %-46s want=%-16s got=%s", mark, r.Path, r.Want, r.Got)
			if r.Msg != "" {
				line += "  (" + r.Msg + ")"
			}
			fmt.Println(line)
		}
	}
}

func solutionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "solution <scenario-id>",
		Short: "Show the reference answer for the current attempt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			_, s, inst, err := resolveStarted(id)
			if err != nil {
				return err
			}
			sol, err := engine.Solution(s, inst)
			if err != nil {
				return err
			}
			fmt.Print(sol)
			return nil
		},
	}
}

func solveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "solve <scenario-id>",
		Short: "Apply the reference solution (to inspect a working answer)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			cfg, s, inst, err := resolveStarted(id)
			if err != nil {
				return err
			}
			if err := engine.ApplySolution(cfg, s, inst); err != nil {
				return err
			}
			fmt.Printf("applied reference solution for %q in namespace %s\n", id, inst.Namespace)
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [scenario-id]",
		Short: "List active scenarios, or re-show the task for one",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return showStatus(args[0])
			}
			return listActive()
		},
	}
}

func listActive() error {
	insts, err := engine.LoadActiveInstances()
	if err != nil {
		return err
	}
	if len(insts) == 0 {
		fmt.Println("no active scenarios")
	} else {
		fmt.Printf("%-26s %-36s %-14s %s\n", "SCENARIO", "NAMESPACE", "VARIANT", "AGE")
		for _, in := range insts {
			fmt.Printf("%-26s %-36s %-14s %s\n", in.ScenarioID, in.Namespace, orDash(in.Variant), ageOf(in.StartedAt))
		}
		fmt.Printf("\n%d active. `status <id>` re-shows a task; `check <id>` grades it.\n", len(insts))
	}
	if exam.InProgress() {
		fmt.Println("\nan exam is in progress — see `ckad-trainer exam status`")
	}
	return nil
}

// showStatus re-renders the task for an active scenario (for when you've lost
// track of what you were working on). It is read-only and never touches the cluster.
func showStatus(id string) error {
	_, scenarios, err := resolveCatalog()
	if err != nil {
		return err
	}
	inst, err := engine.LoadInstance(id)
	if err != nil {
		return err
	}
	s, err := scenario.Find(scenarios, id)
	if err != nil {
		return err
	}
	prompt, err := engine.RenderPrompt(s, inst)
	if err != nil {
		return err
	}
	fmt.Printf("%s  [%s · %s]\n", bold(s.Title), s.Mode, s.Domain)
	fmt.Printf("namespace: %s  %s  started %s ago\n\n",
		inst.Namespace, dim(fmt.Sprintf("(seed %d)", inst.Seed)), ageOf(inst.StartedAt))
	fmt.Println(prompt)
	if len(s.Hints) > 0 {
		fmt.Println("hints:")
		for _, h := range s.Hints {
			fmt.Printf("  - %s\n", h)
		}
	}
	fmt.Printf("\ncheck:    ckad-trainer check %s\n", id)
	fmt.Printf("cleanup:  ckad-trainer cleanup %s\n", id)
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func ageOf(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func cleanupCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "cleanup [scenario-id]",
		Short: "Delete a scenario's namespace and tracked objects (--all for every active one)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if all {
				return cleanupAll(cfg)
			}
			if len(args) != 1 {
				return fmt.Errorf("give a scenario id, or use --all")
			}
			id := args[0]
			inst, err := engine.LoadInstance(id)
			if err != nil {
				return err
			}
			if err := engine.Cleanup(cfg, inst); err != nil {
				return err
			}
			fmt.Printf("cleaned up %q (namespace %s)\n", id, inst.Namespace)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "clean up every active scenario (and end any exam)")
	return cmd
}

func cleanupAll(cfg *config.Config) error {
	insts, err := engine.LoadActiveInstances()
	if err != nil {
		return err
	}
	if len(insts) == 0 && !exam.InProgress() {
		fmt.Println("nothing to clean up")
		return nil
	}
	cleaned := 0
	for _, in := range insts {
		if err := engine.Cleanup(cfg, in); err != nil {
			fmt.Printf("  %s %-26s %v\n", red("warn"), in.ScenarioID, err)
			continue
		}
		fmt.Printf("  cleaned %-26s (namespace %s)\n", in.ScenarioID, in.Namespace)
		cleaned++
	}
	if exam.InProgress() {
		if err := exam.Clear(); err != nil {
			return err
		}
		fmt.Println("  ended the exam session")
	}
	fmt.Printf("done: %d scenario(s) cleaned up\n", cleaned)
	return nil
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available scenarios",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, scenarios, err := resolveCatalog()
			if err != nil {
				return err
			}
			if len(scenarios) == 0 {
				fmt.Println("no scenarios found")
				return nil
			}
			for _, s := range scenarios {
				marker := "  "
				if s.Randomize {
					marker = "🎲" // randomized: values/variants differ each run
				}
				state := ""
				if engine.HasState(s.ID) {
					state = "  (active)"
				}
				fmt.Printf("%s %-26s %-10s %-20s %s%s\n", marker, s.ID, s.Mode, s.Domain, s.Title, state)
			}
			return nil
		},
	}
}

func examCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "exam", Short: "Run a timed, weighted exam session"}
	cmd.AddCommand(examStartCmd(), examStatusCmd(), examGradeCmd(), examAbortCmd())
	return cmd
}

func examStartCmd() *cobra.Command {
	var count, minutes int
	var seed int64
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Sample tasks across domains, start them, and begin the timer",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, scenarios, err := resolveCatalog()
			if err != nil {
				return err
			}
			if count == 0 {
				count = cfg.Defaults.Exam.Count
			}
			if minutes == 0 {
				minutes = cfg.Defaults.Exam.Minutes
			}
			sess, err := exam.Start(cfg, scenarios, count, minutes, seed)
			if err != nil {
				return err
			}
			fmt.Printf("Exam started: %d tasks, %d minutes (deadline %s).\n",
				len(sess.Tasks), minutes, sess.Deadline().Local().Format("15:04 MST"))
			for i, t := range sess.Tasks {
				s, err := scenario.Find(scenarios, t.ScenarioID)
				if err != nil {
					continue
				}
				inst, err := engine.LoadInstance(t.ScenarioID)
				if err != nil {
					continue
				}
				prompt, _ := engine.RenderPrompt(s, inst)
				fmt.Printf("\n──── Task %d/%d  [%s]  ns=%s\n%s",
					i+1, len(sess.Tasks), s.Domain, inst.Namespace, prompt)
			}
			fmt.Println("\nWork the tasks, then: ckad-trainer exam grade   (check time: exam status)")
			return nil
		},
	}
	cmd.Flags().IntVar(&count, "count", 0, "number of tasks (default from config)")
	cmd.Flags().IntVar(&minutes, "minutes", 0, "time limit in minutes (default from config)")
	cmd.Flags().Int64Var(&seed, "seed", 0, "fix the task selection + draws (0 = random)")
	return cmd
}

func examStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show time remaining and how many tasks are passing",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, scenarios, err := resolveCatalog()
			if err != nil {
				return err
			}
			sess, err := exam.Load()
			if err != nil {
				return err
			}
			passing := 0
			for _, t := range sess.Tasks {
				s, ferr := scenario.Find(scenarios, t.ScenarioID)
				if ferr != nil {
					continue
				}
				inst, lerr := engine.LoadInstance(t.ScenarioID)
				if lerr != nil {
					continue
				}
				if report, cerr := engine.CheckQuick(cfg, s, inst); cerr == nil && report.Passed() {
					passing++
				}
			}
			fmt.Printf("Time left: %s   |   passing: %d/%d tasks\n",
				fmtRemaining(sess.Remaining()), passing, len(sess.Tasks))
			return nil
		},
	}
}

func examGradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grade",
		Short: "Score all tasks, show a per-domain breakdown, and clean up",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, scenarios, err := resolveCatalog()
			if err != nil {
				return err
			}
			rep, err := exam.Grade(cfg, scenarios)
			if err != nil {
				return err
			}
			fmt.Println("Tasks:")
			for _, t := range rep.Tasks {
				mark := red("FAIL")
				if t.Passed {
					mark = green("PASS")
				}
				fmt.Printf("  [%s] %-26s %s\n", mark, t.ScenarioID, t.Domain)
			}
			fmt.Println("\nBy domain:")
			for _, d := range rep.Domains {
				fmt.Printf("  %-30s %d/%d\n", d.Domain, d.Passed, d.Total)
			}
			fmt.Printf("\nScore: %.0f%%  (%d/%d tasks, domain-weighted)\n",
				rep.WeightedScore, rep.Passed, rep.Total)
			return nil
		},
	}
}

func examAbortCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "abort",
		Short: "End the exam without scoring and clean up its resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if err := exam.Abort(cfg); err != nil {
				return err
			}
			fmt.Println("exam aborted and cleaned up")
			return nil
		},
	}
}

// fmtRemaining renders a duration as e.g. "47m12s", or "TIME UP" once negative.
func fmtRemaining(d time.Duration) string {
	if d <= 0 {
		return "TIME UP"
	}
	d = d.Round(time.Second)
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func resetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset <scenario-id>",
		Short: "Clean up and restart a scenario with a fresh draw",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, s, err := resolveScenario(args[0])
			if err != nil {
				return err
			}
			return startAndPrint(cfg, s, 0, true)
		},
	}
}

func drillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drill",
		Short: "Flashcard recall drills for kubectl command formats",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, scenarios, err := resolveCatalog()
			if err != nil {
				return err
			}
			var cards []scenario.Scenario
			for _, s := range scenarios {
				if s.Mode == scenario.ModeFlashcard {
					cards = append(cards, s)
				}
			}
			if len(cards) == 0 {
				fmt.Println("no flashcard drills available")
				return nil
			}
			rand.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
			reader := bufio.NewReader(os.Stdin)
			for i, s := range cards {
				fmt.Printf("\n%s [%d/%d]  %s\n", bold("drill"), i+1, len(cards), dim(s.Domain))
				fmt.Print(strings.TrimRight(s.Prompt, "\n"))
				fmt.Print("\n> ")
				_, _ = reader.ReadString('\n') // the attempt is self-graded
				answer := ""
				if len(s.Solution.Commands) > 0 {
					answer = s.Solution.Commands[0]
				}
				fmt.Printf("%s %s\n", green("answer:"), answer)
			}
			fmt.Printf("\n%d cards reviewed.\n", len(cards))
			return nil
		},
	}
}

// --- color output (disabled when piped or NO_COLOR is set) ---

var colorEnabled = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func colorize(code, s string) string {
	if !colorEnabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func green(s string) string { return colorize("32", s) }
func red(s string) string   { return colorize("31", s) }
func bold(s string) string  { return colorize("1", s) }
func dim(s string) string   { return colorize("2", s) }
