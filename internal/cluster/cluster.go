// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package cluster holds cluster-facing helpers: environment diagnostics (doctor)
// and the safety guard that every mutation must pass.
package cluster

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/franciscopaglia/ckad-trainer/internal/config"
	"github.com/franciscopaglia/ckad-trainer/internal/kubectl"
)

// CheckResult is one diagnostic line.
type CheckResult struct {
	Name   string
	OK     bool
	Detail string
}

// Guard returns an error unless it is safe to mutate the cluster: the configured
// context must equal safety.require_context, and the ambient current-context must
// also match it (belt and suspenders). Mutating code paths call this first.
func Guard(cfg *config.Config, kc *kubectl.Client) error {
	if cfg.Cluster.Context != cfg.Safety.RequireContext {
		return fmt.Errorf("refusing to run: cluster.context %q != safety.require_context %q",
			cfg.Cluster.Context, cfg.Safety.RequireContext)
	}
	cur, err := kc.CurrentContext()
	if err != nil {
		return fmt.Errorf("refusing to run: cannot read current context: %w", err)
	}
	if cur != cfg.Safety.RequireContext {
		return fmt.Errorf("refusing to run: current context %q != safety.require_context %q (run: kubectl config use-context %s)",
			cur, cfg.Safety.RequireContext, cfg.Safety.RequireContext)
	}
	return nil
}

// Doctor runs environment checks and reports whether the app is ready to run.
func Doctor(cfg *config.Config) (results []CheckResult, ok bool) {
	kc := kubectl.New(cfg.Cluster.Kubectl, cfg.Cluster.Context)
	add := func(name string, ok bool, detail string) {
		results = append(results, CheckResult{Name: name, OK: ok, Detail: detail})
	}

	// 1. kubectl present + version.
	ver, err := kc.ClientVersion()
	kubectlOK := err == nil
	if kubectlOK {
		add("kubectl", true, ver)
	} else {
		add("kubectl", false, fmt.Sprintf("not found or not runnable: %v", err))
	}

	// 2 & 3. current context vs require_context (the safety guard).
	guardOK := false
	if kubectlOK {
		cur, err := kc.CurrentContext()
		if err != nil {
			add("current-context", false, err.Error())
		} else {
			add("current-context", true, cur)
			guardOK = cur == cfg.Safety.RequireContext
			if guardOK {
				add("context guard", true, fmt.Sprintf("current == require_context (%q)", cfg.Safety.RequireContext))
			} else {
				add("context guard", false, fmt.Sprintf("current %q != require_context %q — mutations will be refused (kubectl config use-context %s)",
					cur, cfg.Safety.RequireContext, cfg.Safety.RequireContext))
			}
		}
	}

	// 4. config invariant: cluster.context must equal require_context.
	if cfg.Cluster.Context == cfg.Safety.RequireContext {
		add("config invariant", true, fmt.Sprintf("cluster.context == require_context (%q)", cfg.Cluster.Context))
	} else {
		add("config invariant", false, fmt.Sprintf("cluster.context %q != require_context %q", cfg.Cluster.Context, cfg.Safety.RequireContext))
		guardOK = false
	}

	// 5. cluster reachable on the configured context.
	reachOK := false
	if kubectlOK {
		if err := kc.Ping(); err != nil {
			add("cluster reachable", false, err.Error())
		} else {
			add("cluster reachable", true, fmt.Sprintf("context %q responds", cfg.Cluster.Context))
			reachOK = true
		}
	}

	// 6. minikube status (informational unless provider == minikube and down).
	if cfg.Cluster.Provider == config.ProviderMinikube {
		status, mok := minikubeStatus()
		if mok {
			add("minikube", true, status)
		} else {
			detail := status
			if cfg.Cluster.AutoStart {
				detail += " (auto_start is on; run scripts/minikube-up.sh)"
			}
			add("minikube", false, detail)
		}
	}

	ok = kubectlOK && guardOK && reachOK
	return results, ok
}

// minikubeStatus shells out to `minikube status`. Returns a one-line summary and
// whether the cluster looks up.
func minikubeStatus() (string, bool) {
	if _, err := exec.LookPath("minikube"); err != nil {
		return "minikube not installed", false
	}
	out, err := exec.Command("minikube", "status", "--format", "{{.Host}}/{{.Kubelet}}/{{.APIServer}}").CombinedOutput()
	summary := strings.TrimSpace(string(out))
	if summary == "" {
		summary = "no status"
	}
	// `minikube status` exits non-zero when the cluster is stopped.
	up := err == nil && strings.Contains(summary, "Running")
	return summary, up
}
