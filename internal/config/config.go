// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package config loads and validates the ckad-trainer configuration file.
//
// The config is a single YAML file (see config.example.yaml). Load applies
// defaults and then enforces the invariants the rest of the app relies on —
// most importantly that a target context and a safety guard context are set.
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the parsed configuration. Field tags match config.example.yaml.
type Config struct {
	Cluster struct {
		Provider  string `yaml:"provider"`   // "minikube" | "kubeconfig"
		Context   string `yaml:"context"`    // REQUIRED: kube context we operate on
		Kubectl   string `yaml:"kubectl"`    // kubectl binary (default "kubectl")
		AutoStart bool   `yaml:"auto_start"` // minikube provider: start if down
	} `yaml:"cluster"`
	NamespacePrefix string `yaml:"namespace_prefix"`
	Defaults        struct {
		Exam struct {
			Count   int `yaml:"count"`
			Minutes int `yaml:"minutes"`
		} `yaml:"exam"`
	} `yaml:"defaults"`
	Safety struct {
		// RequireContext gates every mutation: we refuse to act unless this
		// matches the context we are about to use. REQUIRED.
		RequireContext string `yaml:"require_context"`
	} `yaml:"safety"`
	// ScenarioDir, when non-empty, loads scenarios from disk instead of the
	// embedded catalog (used while authoring).
	ScenarioDir string `yaml:"scenario_dir"`
}

// Provider constants.
const (
	ProviderMinikube   = "minikube"
	ProviderKubeconfig = "kubeconfig"
)

// ErrNotFound is returned by Load when the config file does not exist, so callers
// can fall back to auto-detecting the current kube context.
var ErrNotFound = errors.New("config file not found")

// Load reads path, applies defaults, and validates. When the file is missing it
// returns ErrNotFound (callers may then fall back to Default).
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return &c, nil
}

// Default builds a zero-config configuration that operates on the given kube
// context. Used when there is no config file: the app simply uses whatever
// context kubectl is currently pointed at, and pins the safety guard to it so it
// only touches the cluster you're already on.
func Default(context string) *Config {
	var c Config
	c.Cluster.Provider = ProviderKubeconfig
	c.Cluster.Context = context
	c.Safety.RequireContext = context
	c.applyDefaults()
	return &c
}

// Template renders a starter config.yaml pinned to the given context.
func Template(context string) string {
	return fmt.Sprintf(`cluster:
  provider: kubeconfig
  context: %s
  kubectl: kubectl
namespace_prefix: ckad
defaults:
  exam:
    count: 16
    minutes: 120
safety:
  require_context: %s
# scenario_dir: ./scenarios   # uncomment to load scenarios from disk (authoring)
`, context, context)
}

// applyDefaults fills in optional fields that were left empty.
func (c *Config) applyDefaults() {
	if c.Cluster.Provider == "" {
		c.Cluster.Provider = ProviderMinikube
	}
	if c.Cluster.Kubectl == "" {
		c.Cluster.Kubectl = "kubectl"
	}
	if c.NamespacePrefix == "" {
		c.NamespacePrefix = "ckad"
	}
	if c.Defaults.Exam.Count == 0 {
		c.Defaults.Exam.Count = 16
	}
	if c.Defaults.Exam.Minutes == 0 {
		c.Defaults.Exam.Minutes = 120
	}
}

// Validate enforces invariants the app depends on. It returns the first problem
// found, naming the offending field.
func (c *Config) Validate() error {
	if c.Cluster.Context == "" {
		return errors.New("cluster.context is required (the kube context to operate on)")
	}
	if c.Safety.RequireContext == "" {
		return errors.New("safety.require_context is required (the only context this app may touch)")
	}
	switch c.Cluster.Provider {
	case ProviderMinikube, ProviderKubeconfig:
	default:
		return fmt.Errorf("cluster.provider %q must be %q or %q", c.Cluster.Provider, ProviderMinikube, ProviderKubeconfig)
	}
	return nil
}
