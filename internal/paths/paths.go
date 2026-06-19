// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package paths resolves where ckad-trainer keeps its config and per-scenario
// state. A binary on $PATH must not depend on the current directory, so state
// and config live in stable per-user locations (XDG), while a ./config.yaml in
// the working directory is still honored for development and authoring.
package paths

import (
	"os"
	"path/filepath"
)

// appName is the per-user subdirectory under the XDG roots.
const appName = "ckad-trainer"

// localConfig is the config filename looked for in the current directory.
const localConfig = "config.yaml"

// StateDir is where per-scenario state files (and the exam session) live.
// Override with CKAD_TRAINER_STATE; otherwise $XDG_STATE_HOME/ckad-trainer,
// falling back to ~/.local/state/ckad-trainer.
func StateDir() string {
	if d := os.Getenv("CKAD_TRAINER_STATE"); d != "" {
		return d
	}
	return filepath.Join(xdg("XDG_STATE_HOME", ".local", "state"), appName)
}

// ConfigDir is the per-user config directory ($XDG_CONFIG_HOME/ckad-trainer,
// falling back to ~/.config/ckad-trainer).
func ConfigDir() string {
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), appName)
}

// DefaultConfigFile is the per-user config file path.
func DefaultConfigFile() string {
	return filepath.Join(ConfigDir(), localConfig)
}

// ResolveConfig decides which config file to use given the value of the
// --config flag (empty means "not set"). Precedence:
//  1. an explicit --config value;
//  2. ./config.yaml in the working directory, if it exists (dev/authoring);
//  3. the per-user config file (created by `init`).
func ResolveConfig(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if fileExists(localConfig) {
		return localConfig
	}
	return DefaultConfigFile()
}

// xdg returns $envVar if set, else $HOME joined with the fallback elements
// (e.g. ".local", "state"). When HOME is unknown it returns a relative path so
// the tool still works, just rooted at the current directory.
func xdg(envVar string, fallback ...string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(fallback...)
	}
	return filepath.Join(append([]string{home}, fallback...)...)
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
