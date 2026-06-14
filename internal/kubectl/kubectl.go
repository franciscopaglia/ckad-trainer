// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

// Package kubectl is a thin os/exec wrapper around the kubectl binary.
//
// Every command that talks to the cluster goes through run, which ALWAYS injects
// an explicit --context so we never rely on the ambient current-context for
// anything we do. This is a safety property: the rest of the app cannot
// accidentally operate on the wrong cluster.
package kubectl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Client runs kubectl against a fixed context.
type Client struct {
	Bin     string // kubectl binary, e.g. "kubectl"
	Context string // --context value injected into every cluster call
}

// New constructs a Client. bin defaults to "kubectl" when empty.
func New(bin, context string) *Client {
	if bin == "" {
		bin = "kubectl"
	}
	return &Client{Bin: bin, Context: context}
}

// run executes kubectl with --context injected, returning stdout. On failure the
// error includes stderr so callers (and users) see why.
func (c *Client) run(args ...string) ([]byte, error) {
	full := append([]string{"--context", c.Context}, args...)
	cmd := exec.Command(c.Bin, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("kubectl %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// runStdin is like run but feeds stdin (used by Apply).
func (c *Client) runStdin(stdin string, args ...string) ([]byte, error) {
	full := append([]string{"--context", c.Context}, args...)
	cmd := exec.Command(c.Bin, full...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("kubectl %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// CurrentContext returns the ambient current-context. It deliberately does NOT
// inject --context (that flag is meaningless for this query).
func (c *Client) CurrentContext() (string, error) {
	cmd := exec.Command(c.Bin, "config", "current-context")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting current-context: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ClientVersion returns kubectl's client version string (no server contact).
func (c *Client) ClientVersion() (string, error) {
	cmd := exec.Command(c.Bin, "version", "--client", "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting kubectl version: %w", err)
	}
	var v struct {
		ClientVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"clientVersion"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return "", fmt.Errorf("parsing kubectl version: %w", err)
	}
	return v.ClientVersion.GitVersion, nil
}

// Ping verifies the configured context can reach the API server.
func (c *Client) Ping() error {
	_, err := c.run("get", "namespace", "--request-timeout=8s", "-o", "name")
	return err
}

// GetJSON fetches one object as a decoded JSON map. ns == "" means cluster-scoped.
func (c *Client) GetJSON(kind, name, ns string) (map[string]any, error) {
	args := []string{"get", kind, name, "-o", "json"}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		return nil, fmt.Errorf("decoding %s/%s json: %w", kind, name, err)
	}
	return obj, nil
}

// ListJSON lists objects of a kind, returning each item's JSON map. selector is
// an optional label selector ("" for none).
func (c *Client) ListJSON(kind, ns, selector string) ([]map[string]any, error) {
	args := []string{"get", kind, "-o", "json"}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	if selector != "" {
		args = append(args, "-l", selector)
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("decoding %s list json: %w", kind, err)
	}
	return list.Items, nil
}

// Apply applies a manifest (YAML or JSON) supplied on stdin.
func (c *Client) Apply(manifest string) error {
	_, err := c.runStdin(manifest, "apply", "-f", "-")
	return err
}

// CreateNamespace creates a namespace with the given labels (applied, so it is
// idempotent).
func (c *Client) CreateNamespace(ns string, labels map[string]string) error {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ")
	b.WriteString(ns)
	b.WriteString("\n")
	if len(labels) > 0 {
		b.WriteString("  labels:\n")
		for k, v := range labels {
			fmt.Fprintf(&b, "    %s: %q\n", k, v)
		}
	}
	return c.Apply(b.String())
}

// DeleteNamespace deletes a namespace if present, returning immediately.
func (c *Client) DeleteNamespace(ns string) error {
	_, err := c.run("delete", "namespace", ns, "--ignore-not-found", "--wait=false")
	return err
}

// Delete removes a single object if present. ns == "" for cluster-scoped kinds.
func (c *Client) Delete(kind, name, ns string) error {
	args := []string{"delete", kind, name, "--ignore-not-found"}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	_, err := c.run(args...)
	return err
}

// Raw runs an arbitrary kubectl invocation (args exclude the kubectl binary) with
// --context injected. Used for scenario setup.commands and command drills.
func (c *Client) Raw(args ...string) ([]byte, error) {
	return c.run(args...)
}
