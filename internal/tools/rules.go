package tools

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

// Evaluate returns the action and reason for command.
// Empty action means no rule matched.
func (rs *RuleSet) Evaluate(command string) (string, string) {
	for _, rule := range rs.Rules {
		if matchGlob(rule.Match, command) {
			return rule.Action, rule.Reason
		}
	}
	return "", ""
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
