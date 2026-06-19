// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateDirEnvOverride(t *testing.T) {
	t.Setenv("CKAD_TRAINER_STATE", "/tmp/ckad-state")
	if got := StateDir(); got != "/tmp/ckad-state" {
		t.Errorf("StateDir = %q, want /tmp/ckad-state", got)
	}
}

func TestStateDirXDG(t *testing.T) {
	t.Setenv("CKAD_TRAINER_STATE", "")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	want := filepath.Join("/xdg/state", "ckad-trainer")
	if got := StateDir(); got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
}

func TestConfigDirXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	want := filepath.Join("/xdg/config", "ckad-trainer")
	if got := ConfigDir(); got != want {
		t.Errorf("ConfigDir = %q, want %q", got, want)
	}
}

func TestResolveConfigExplicitWins(t *testing.T) {
	if got := ResolveConfig("/etc/my.yaml"); got != "/etc/my.yaml" {
		t.Errorf("ResolveConfig(explicit) = %q", got)
	}
}

func TestResolveConfigPrefersLocalFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := os.WriteFile("config.yaml", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveConfig(""); got != localConfig {
		t.Errorf("ResolveConfig with local file = %q, want %q", got, localConfig)
	}
}

func TestResolveConfigFallsBackToXDG(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir) // empty dir: no ./config.yaml
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	want := filepath.Join("/xdg/config", "ckad-trainer", "config.yaml")
	if got := ResolveConfig(""); got != want {
		t.Errorf("ResolveConfig fallback = %q, want %q", got, want)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}
