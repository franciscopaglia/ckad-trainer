// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsErrNotFound(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDefaultIsValid(t *testing.T) {
	c := Default("mctx")
	if err := c.Validate(); err != nil {
		t.Fatalf("Default() produced invalid config: %v", err)
	}
	if c.Cluster.Context != "mctx" || c.Safety.RequireContext != "mctx" {
		t.Errorf("context/require_context not pinned to mctx: %+v", c.Cluster)
	}
	if c.Cluster.Provider != ProviderKubeconfig {
		t.Errorf("provider = %q, want %q", c.Cluster.Provider, ProviderKubeconfig)
	}
}

func TestTemplateLoadsBack(t *testing.T) {
	p := writeTemp(t, Template("mctx"))
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load(Template): %v", err)
	}
	if c.Cluster.Context != "mctx" || c.Safety.RequireContext != "mctx" {
		t.Errorf("round-trip context wrong: %+v", c.Cluster)
	}
}

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAppliesDefaults(t *testing.T) {
	p := writeTemp(t, `
cluster:
  context: minikube
safety:
  require_context: minikube
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Cluster.Provider != ProviderMinikube {
		t.Errorf("provider default = %q, want %q", c.Cluster.Provider, ProviderMinikube)
	}
	if c.Cluster.Kubectl != "kubectl" {
		t.Errorf("kubectl default = %q, want kubectl", c.Cluster.Kubectl)
	}
	if c.NamespacePrefix != "ckad" {
		t.Errorf("namespace_prefix default = %q, want ckad", c.NamespacePrefix)
	}
	if c.Defaults.Exam.Count != 16 || c.Defaults.Exam.Minutes != 120 {
		t.Errorf("exam defaults = %d/%d, want 16/120", c.Defaults.Exam.Count, c.Defaults.Exam.Minutes)
	}
}

func TestValidateRejectsMissingContext(t *testing.T) {
	p := writeTemp(t, `
safety:
  require_context: minikube
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing cluster.context, got nil")
	}
}

func TestValidateRejectsMissingRequireContext(t *testing.T) {
	p := writeTemp(t, `
cluster:
  context: minikube
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing safety.require_context, got nil")
	}
}

func TestValidateRejectsBadProvider(t *testing.T) {
	p := writeTemp(t, `
cluster:
  context: minikube
  provider: aws
safety:
  require_context: minikube
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
