package tools

import (
	"fmt"
	"log/slog"
)

// Returns an error if the permission level denies execution.
// "ask" logs a warning and allows (v1 behavior).
func CheckPermission(level string) error {
	switch level {
	case "deny":
		return fmt.Errorf("tool: permission denied")
	case "ask":
		slog.Warn("tool: ask permission, allowing in v1",
			"level", level,
		)

		return nil
	case "allow":

		return nil
	default:

		return fmt.Errorf("tool: unknown permission level %q", level)
	}
}

// MergeRules merges custom rules followed by preset rules into a single
// RuleSet. Custom rules are placed first so they take precedence via
// first-match-wins evaluation. Preset names not found in Presets are
// silently skipped.
func MergeRules(presetNames []string, custom []PermissionRule) RuleSet {
	var rs RuleSet
	rs.Rules = append(rs.Rules, custom...)
	for _, name := range presetNames {
		if preset, ok := Presets[name]; ok {
			rs.Rules = append(rs.Rules, preset...)
		}
	}
	return rs
}

// EvaluateToolPermission implements the full permission flow:
//
//	tool-level → rule set → effective action
//
// Returns the effective action and matching reason.
// toolPerm is "allow", "ask", or "deny".
// An empty ruleSet evaluates all commands as the tool-level action.
func EvaluateToolPermission(toolPerm string, command string, rules *RuleSet) (string, string, error) {
	if toolPerm == "deny" {
		return "deny", "", fmt.Errorf("tool: permission denied")
	}

	if rules != nil {
		ruleAction, ruleReason := rules.Evaluate(command)
		switch ruleAction {
		case "deny":
			r := ruleReason
			if r == "" {
				r = "rule denied"
			}
			return "deny", r, fmt.Errorf("rule: %s", r)
		case "allow":
			return "allow", ruleReason, nil
		case "ask":
			return "ask", ruleReason, nil
		}
	}

	// No rule matched; fall back to tool-level action.
	if toolPerm == "ask" {
		return "ask", "", nil
	}
	return "allow", "", nil
}
