// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package verify evaluates scenario assertions against a live object's JSON.
//
// Paths use kubectl JSONPath syntax (e.g. `{.spec.containers[0].image}`) and are
// evaluated with the SAME engine kubectl uses (k8s.io/client-go/util/jsonpath),
// so a path that works in `kubectl get -o jsonpath=` works here.
package verify

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/util/jsonpath"

	"github.com/franciscopaglia/ckad-trainer/internal/scenario"
)

// Result is the outcome of one assertion.
type Result struct {
	Path string
	Want string
	Got  string
	Msg  string
	Pass bool
}

// Report bundles the results of a set of assertions.
type Report struct {
	Results []Result
}

// Passed reports whether every result passed (an empty report passes).
func (r Report) Passed() bool { return AllPass(r.Results) }

// AllPass reports whether every result passed (an empty slice passes).
func AllPass(results []Result) bool {
	for _, res := range results {
		if !res.Pass {
			return false
		}
	}
	return true
}

// Evaluate runs each assert against obj. A nil/empty obj (object missing) yields
// failing results with Got "<missing>".
func Evaluate(obj map[string]any, asserts []scenario.Assert) []Result {
	out := make([]Result, 0, len(asserts))
	for _, a := range asserts {
		out = append(out, evalOne(obj, a))
	}
	return out
}

func evalOne(obj map[string]any, a scenario.Assert) Result {
	r := Result{Path: a.Path}
	if obj == nil {
		r.Got = "<missing>"
		r.Want, r.Msg = describeWant(a), "object not found"
		return r
	}
	got, err := evalPath(obj, a.Path)
	if err != nil {
		r.Got = "<error>"
		r.Msg = err.Error()
		return r
	}
	return applyMatcher(a, got)
}

// evalPath returns the string form of every value the path selects.
func evalPath(obj map[string]any, path string) ([]string, error) {
	jp := jsonpath.New("v").AllowMissingKeys(true)
	if err := jp.Parse(path); err != nil {
		return nil, fmt.Errorf("bad jsonpath %q: %w", path, err)
	}
	groups, err := jp.FindResults(obj)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, g := range groups {
		for _, v := range g {
			out = append(out, valToString(v))
		}
	}
	return out, nil
}

func valToString(v reflect.Value) string {
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return ""
	}
	if v.Kind() == reflect.Float64 {
		f := v.Float()
		if f == math.Trunc(f) && !math.IsInf(f, 0) {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return fmt.Sprintf("%v", v.Interface())
}

func applyMatcher(a scenario.Assert, got []string) Result {
	gotStr := strings.Join(got, ",")
	first := ""
	if len(got) > 0 {
		first = got[0]
	}
	r := Result{Path: a.Path, Got: gotStr, Want: describeWant(a)}

	switch {
	case a.Exists != nil:
		present := len(got) > 0
		r.Pass = present == *a.Exists
		if !r.Pass {
			r.Msg = fmt.Sprintf("exists=%v but present=%v", *a.Exists, present)
		}
	case a.Equals != "":
		r.Pass = len(got) == 1 && valueEqual(first, a.Equals)
	case a.NotEquals != "":
		r.Pass = !(len(got) == 1 && valueEqual(first, a.NotEquals))
	case a.Contains != "":
		r.Pass = contains(got, a.Contains)
	case a.NotContains != "":
		r.Pass = !contains(got, a.NotContains)
	case len(a.In) > 0:
		r.Pass = len(got) >= 1 && contains(a.In, first)
	case a.Matches != "":
		re, err := regexp.Compile(a.Matches)
		if err != nil {
			r.Msg = "bad regex: " + err.Error()
		} else {
			r.Pass = re.MatchString(first)
		}
	case a.Gte != "":
		r.Pass, r.Msg = cmpNum(first, a.Gte, ">=")
	case a.Lte != "":
		r.Pass, r.Msg = cmpNum(first, a.Lte, "<=")
	default:
		r.Msg = "no matcher set"
	}

	if r.Got == "" {
		r.Got = "<empty>"
	}
	return r
}

func describeWant(a scenario.Assert) string {
	switch {
	case a.Exists != nil:
		return fmt.Sprintf("exists=%v", *a.Exists)
	case a.Equals != "":
		return a.Equals
	case a.NotEquals != "":
		return "!= " + a.NotEquals
	case a.Contains != "":
		return "contains " + a.Contains
	case a.NotContains != "":
		return "!contains " + a.NotContains
	case len(a.In) > 0:
		return "in [" + strings.Join(a.In, ", ") + "]"
	case a.Matches != "":
		return "=~ " + a.Matches
	case a.Gte != "":
		return ">= " + a.Gte
	case a.Lte != "":
		return "<= " + a.Lte
	}
	return "?"
}

// valueEqual reports string equality, with a quantity-aware fallback so that
// equivalent resource quantities match regardless of how they were written
// (e.g. cpu "500m" == "0.5", memory "1Gi" == "1024Mi"). The fallback only fires
// when BOTH sides parse as Kubernetes quantities, so non-numeric fields (names,
// images, phases) keep strict string semantics.
func valueEqual(got, want string) bool {
	if got == want {
		return true
	}
	gq, eg := resource.ParseQuantity(got)
	wq, ew := resource.ParseQuantity(want)
	return eg == nil && ew == nil && gq.Cmp(wq) == 0
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// cmpNum compares two values, quantity-aware (1Gi, 100m) with a numeric fallback.
func cmpNum(gotS, wantS, op string) (bool, string) {
	c, err := compareQuantity(gotS, wantS)
	if err != nil {
		return false, err.Error()
	}
	switch op {
	case ">=":
		return c >= 0, ""
	case "<=":
		return c <= 0, ""
	}
	return false, "bad op"
}

func compareQuantity(gotS, wantS string) (int, error) {
	gq, e1 := resource.ParseQuantity(gotS)
	wq, e2 := resource.ParseQuantity(wantS)
	if e1 == nil && e2 == nil {
		return gq.Cmp(wq), nil
	}
	gf, e3 := strconv.ParseFloat(gotS, 64)
	wf, e4 := strconv.ParseFloat(wantS, 64)
	if e3 == nil && e4 == nil {
		switch {
		case gf < wf:
			return -1, nil
		case gf > wf:
			return 1, nil
		default:
			return 0, nil
		}
	}
	return 0, fmt.Errorf("cannot compare %q and %q numerically", gotS, wantS)
}
