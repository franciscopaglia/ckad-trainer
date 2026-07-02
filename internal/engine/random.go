// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

// draw resolves a scenario into a concrete variant + parameter values using the
// seeded RNG. It is deterministic: the same seed yields the same variant and the
// same params (params are resolved in sorted key order so map iteration cannot
// affect the result).
func draw(s scenario.Scenario, rng *rand.Rand) (variant string, params map[string]string, err error) {
	params = map[string]string{}

	// 1. Pick a variant (weighted) if any exist.
	defs := map[string]scenario.Param{}
	for k, v := range s.Params {
		defs[k] = v
	}
	if len(s.Variants) > 0 {
		v := pickWeightedVariant(s.Variants, rng)
		variant = v.Name
		for k, p := range v.Params { // variant-local params override top-level
			defs[k] = p
		}
	}

	// 2. Resolve params in sorted order so draws are reproducible. Later params
	//    may reference earlier ones via the data map.
	data := map[string]any{}
	for _, k := range sortedKeys(defs) {
		val, rerr := resolveParam(defs[k], rng, data)
		if rerr != nil {
			return "", nil, fmt.Errorf("param %q: %w", k, rerr)
		}
		params[k] = val
		data[k] = val
	}
	return variant, params, nil
}

// CheckDraw resolves a scenario with the given seed and renders every templated
// field, returning the first error. It is cluster-free and is used by the
// catalog validation test to ensure scenarios render for any draw.
func CheckDraw(s scenario.Scenario, seed int64) error {
	if seed == 0 {
		seed = 1
	}
	rng := rand.New(rand.NewSource(seed))
	variant, params, err := draw(s, rng)
	if err != nil {
		return err
	}
	inst := &Instance{ScenarioID: s.ID, Namespace: "ns-validate", Variant: variant, Params: params}
	data := inst.data()

	prompt := s.Prompt
	if v, ok := findVariant(s, variant); ok {
		prompt = v.Prompt
	}
	templates := append([]string{prompt}, s.Hints...)
	templates = append(templates, s.Setup.Manifests...)
	templates = append(templates, s.Setup.Commands...)
	templates = append(templates, s.Solution.Manifests...)
	templates = append(templates, s.Solution.Commands...)
	templates = append(templates, s.Solution.Description)
	for _, ref := range s.Cleanup.ClusterScoped {
		templates = append(templates, ref.Name)
	}
	templates = append(templates, s.Cleanup.Commands...)
	for _, t := range templates {
		if _, err := render(t, data); err != nil {
			return err
		}
	}
	for _, c := range resolveChecks(s, inst) {
		if _, err := renderCheck(c, data); err != nil {
			return err
		}
	}
	return nil
}

// pickWeightedVariant chooses a variant with probability proportional to its
// Weight (default 1).
func pickWeightedVariant(vs []scenario.Variant, rng *rand.Rand) scenario.Variant {
	total := 0
	for _, v := range vs {
		total += variantWeight(v)
	}
	r := rng.Intn(total)
	for _, v := range vs {
		if r < variantWeight(v) {
			return v
		}
		r -= variantWeight(v)
	}
	return vs[len(vs)-1]
}

func variantWeight(v scenario.Variant) int {
	if v.Weight <= 0 {
		return 1
	}
	return v.Weight
}

// resolveParam draws a single parameter value.
func resolveParam(p scenario.Param, rng *rand.Rand, data map[string]any) (string, error) {
	switch {
	case len(p.Pick) > 0:
		return p.Pick[rng.Intn(len(p.Pick))], nil
	case len(p.Choice) > 0:
		return p.Choice[rng.Intn(len(p.Choice))], nil
	case p.Range != nil:
		return drawRange(*p.Range, rng), nil
	case p.Pattern != "":
		return renderPattern(p.Pattern, rng, data)
	default:
		return "", fmt.Errorf("must set one of pick/choice/range/pattern")
	}
}

func drawRange(r scenario.Range, rng *rand.Rand) string {
	step := r.Step
	if step <= 0 {
		step = 1
	}
	n := (r.Max-r.Min)/step + 1
	if n < 1 {
		n = 1
	}
	return strconv.Itoa(r.Min + step*rng.Intn(n))
}

// renderPattern renders a pattern string with the random-helper funcs, so a
// param can be e.g. `consumer-{{randInt 100 999}}`.
func renderPattern(pat string, rng *rand.Rand, data map[string]any) (string, error) {
	funcs := template.FuncMap{
		"randInt":  func(min, max int) int { return min + rng.Intn(max-min+1) },
		"randPick": func(items ...string) string { return items[rng.Intn(len(items))] },
		"randName": func(prefix string) string { return prefix + "-" + randIDFrom(rng, 5) },
	}
	t, err := template.New("p").Funcs(funcs).Option("missingkey=error").Parse(pat)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func sortedKeys(m map[string]scenario.Param) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// randIDFrom generates an n-char id from the given (seeded) RNG.
func randIDFrom(rng *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = idAlphabet[rng.Intn(len(idAlphabet))]
	}
	return string(b)
}
