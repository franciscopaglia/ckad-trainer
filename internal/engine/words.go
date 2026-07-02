// Copyright (C) 2026 Francisco Paglia
// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"strings"
)

// splitWords splits a command line into arguments with shell-like quoting:
// whitespace separates words; single quotes preserve everything literally;
// double quotes preserve whitespace and allow \" and \\ escapes; outside
// quotes a backslash escapes the next character. Scenario commands are run
// without a shell (argv passed straight to kubectl), so this is what makes
// quoted values like --from-literal=key="a b" work at all — plain
// strings.Fields would tear them apart.
func splitWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
			i++
		case c == '\'':
			inWord = true
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("unbalanced single quote in %q", s)
			}
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 2
		case c == '"':
			inWord = true
			i++
			closed := false
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
					cur.WriteByte(s[i+1])
					i += 2
					continue
				}
				if s[i] == '"' {
					closed = true
					i++
					break
				}
				cur.WriteByte(s[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unbalanced double quote in %q", s)
			}
		case c == '\\' && i+1 < len(s):
			inWord = true
			cur.WriteByte(s[i+1])
			i += 2
		default:
			inWord = true
			cur.WriteByte(c)
			i++
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// kubectlArgs splits a scenario command and strips a leading "kubectl", giving
// the argv for the context-injecting kubectl client. Empty and comment-only
// commands return nil.
func kubectlArgs(command string) ([]string, error) {
	line := strings.TrimSpace(command)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil
	}
	args, err := splitWords(line)
	if err != nil {
		return nil, err
	}
	if len(args) > 0 && args[0] == "kubectl" {
		args = args[1:]
	}
	return args, nil
}
