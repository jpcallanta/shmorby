package tools

import (
	"regexp"
	"strings"
)

// PermissionRule maps a command glob pattern to an action.
type PermissionRule struct {
	Match  string `yaml:"match"`
	Action string `yaml:"action"`
	Reason string `yaml:"reason"`
}

// RuleSet holds ordered rules evaluated top-to-bottom, first match wins.
type RuleSet struct {
	Rules []PermissionRule
}

// Evaluate returns the action, reason, and matched pattern for command.
// Empty action means no rule matched.
func (rs *RuleSet) Evaluate(command string) (string, string, string) {
	normalized := normalizeCommand(command)
	for _, rule := range rs.Rules {
		if matchGlob(rule.Match, normalized) {
			return rule.Action, rule.Reason, rule.Match
		}
	}
	return "", "", ""
}

// collapseWS is compiled once at package level.
var collapseWS = regexp.MustCompile(`\s+`)

// normalizeCommand reduces a command string to a canonical form so
// that glob-based permission rules cannot be bypassed via trivial
// formatting tricks: extra whitespace, absolute path prefixes, or
// env prefix wrappers.
//
// Without normalization, deny rules like "rm -rf *" can be evaded
// by "rm  -rf /", "/usr/bin/rm -rf /", or "env rm -rf /".
//
// NOTE: The prefix list covers the most common cases but does not
// exhaustively cover every possible PATH entry (e.g.
// /usr/local/bin/env, /snap/bin/env). An attacker with a custom
// PATH entry could still evade normalization. This is an acceptable
// residual gap because the primary attack vector (trivial
// reformatting) is closed, and exotic PATH entries are uncommon in
// practice.
func normalizeCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)

	// Collapse runs of whitespace (tabs, multiple spaces) to a
	// single space so "rm  -rf /" matches "rm -rf *".
	cmd = collapseWS.ReplaceAllString(cmd, " ")

	// Strip well-known PATH prefixes so "/usr/bin/rm -rf /" and
	// "env rm -rf /" match the same pattern as "rm -rf /".
	for _, prefix := range []string{
		"env ",
		"/usr/bin/",
		"/bin/",
		"/usr/sbin/",
		"/sbin/",
	} {
		if strings.HasPrefix(cmd, prefix) {
			cmd = cmd[len(prefix):]
			break
		}
	}

	return cmd
}

// matchGlob reports whether pattern matches command.
// Supports * (any chars) and ? (single char). Unlike filepath.Match,
// * matches / like shell glob.
func matchGlob(pattern, command string) bool {
	for len(pattern) > 0 && len(command) > 0 {
		if pattern[0] == '*' {
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(command); i++ {
				if matchGlob(pattern, command[i:]) {
					return true
				}
			}
			return false
		}
		if pattern[0] == '?' {
			command = command[1:]
			pattern = pattern[1:]
			continue
		}
		if pattern[0] != command[0] {
			return false
		}
		command = command[1:]
		pattern = pattern[1:]
	}
	for len(pattern) > 0 && pattern[0] == '*' {
		pattern = pattern[1:]
	}
	return len(pattern) == 0 && len(command) == 0
}
