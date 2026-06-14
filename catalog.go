// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package catalog ships the scenario files embedded in the binary, so a single
// `ckad-trainer` executable carries its whole catalog. During authoring, set
// `scenario_dir` in config.yaml to load scenarios from disk instead.
package catalog

import (
	"embed"
	"io/fs"
)

//go:embed scenarios
var embedded embed.FS

// FS returns the embedded catalog rooted at the scenarios directory (so callers
// see `practice/...` directly, matching an on-disk `scenarios/` layout).
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "scenarios")
	if err != nil {
		panic("embedded scenarios missing: " + err.Error()) // compile-time guaranteed
	}
	return sub
}
