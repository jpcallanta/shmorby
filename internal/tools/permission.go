package tools

import "fmt"

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
// Returns the effective action, matching reason, matched pattern,
// and rule action (the action from the matched rule).
// toolPerm is "allow", "ask", or "deny".
// An empty ruleSet evaluates all commands as the tool-level action.
func EvaluateToolPermission(toolPerm string, command string, rules *RuleSet) (string, string, string, string, error) {
	if toolPerm == "deny" {
		return "deny", "", "", "", fmt.Errorf("tool: permission denied")
	}

	if rules != nil {
		ruleAction, ruleReason, matchedPattern := rules.Evaluate(command)
		switch ruleAction {
		case "deny":
			r := ruleReason
			if r == "" {
				r = "rule denied"
			}
			return "deny", r, matchedPattern, ruleAction, fmt.Errorf("rule: %s", r)
		case "allow":
			return "allow", ruleReason, matchedPattern, ruleAction, nil
		case "ask":
			return "ask", ruleReason, matchedPattern, ruleAction, nil
		}
	}

	// No rule matched; fall back to tool-level action.
	if toolPerm == "ask" {
		return "ask", "", "", "", nil
	}
	return "allow", "", "", "", nil
}
