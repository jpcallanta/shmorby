package tools

import "testing"

// TestRuleSet_Evaluate_ExactMatch checks exact command matching.
func TestRuleSet_Evaluate_ExactMatch(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf /", Action: "deny", Reason: "root destruction"},
	}}

	action, reason, _ := rs.Evaluate("rm -rf /")
	if action != "deny" {
		t.Errorf("want deny, got %q", action)
	}
	if reason != "root destruction" {
		t.Errorf("want 'root destruction', got %q", reason)
	}
}

// TestRuleSet_Evaluate_Wildcard checks wildcard matching.
func TestRuleSet_Evaluate_Wildcard(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "systemctl restart *", Action: "ask", Reason: "service restart"},
	}}

	action, reason, _ := rs.Evaluate("systemctl restart nginx")
	if action != "ask" {
		t.Errorf("want ask, got %q", action)
	}
	if reason != "service restart" {
		t.Errorf("want 'service restart', got %q", reason)
	}
}

// TestRuleSet_Evaluate_PrefixWildcard checks prefix wildcard matching.
func TestRuleSet_Evaluate_PrefixWildcard(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "aws ec2 describe-*", Action: "allow"},
	}}

	action, _, _ := rs.Evaluate("aws ec2 describe-instances")
	if action != "allow" {
		t.Errorf("want allow, got %q", action)
	}
}

// TestRuleSet_Evaluate_FirstMatchWins checks top-to-bottom ordering.
func TestRuleSet_Evaluate_FirstMatchWins(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm *", Action: "deny", Reason: "block all rm"},
		{Match: "rm -rf /", Action: "allow"},
	}}

	action, reason, _ := rs.Evaluate("rm -rf /")
	if action != "deny" {
		t.Errorf("want deny (first match), got %q", action)
	}
	if reason != "block all rm" {
		t.Errorf("want 'block all rm', got %q", reason)
	}
}

// TestRuleSet_Evaluate_NoMatch checks empty result when nothing matches.
func TestRuleSet_Evaluate_NoMatch(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm *", Action: "deny"},
	}}

	action, _, _ := rs.Evaluate("ls -la")
	if action != "" {
		t.Errorf("want empty, got %q", action)
	}
}

// TestRuleSet_Evaluate_EmptyRules checks empty rule set returns empty.
func TestRuleSet_Evaluate_EmptyRules(t *testing.T) {
	rs := RuleSet{}

	action, _, _ := rs.Evaluate("anything")
	if action != "" {
		t.Errorf("want empty, got %q", action)
	}
}

// TestMatchGlob_StarOnly checks * matches everything.
func TestMatchGlob_StarOnly(t *testing.T) {
	if !matchGlob("*", "anything") {
		t.Error("want true for * matching anything")
	}
}

// TestMatchGlob_Exact checks exact match.
func TestMatchGlob_Exact(t *testing.T) {
	if !matchGlob("rm -rf /", "rm -rf /") {
		t.Error("want true for exact match")
	}
}

// TestMatchGlob_TrailingStar checks prefix + star.
func TestMatchGlob_TrailingStar(t *testing.T) {
	if !matchGlob("systemctl restart *", "systemctl restart nginx") {
		t.Error("want true for prefix+star match")
	}
}

// TestMatchGlob_TrailingStarNoMatch checks prefix mismatch with star.
func TestMatchGlob_TrailingStarNoMatch(t *testing.T) {
	if matchGlob("apt install *", "yum install foo") {
		t.Error("want false for mismatched prefix")
	}
}

// TestEvaluateToolPermission_DenyToolLevel checks tool-level deny.
func TestEvaluateToolPermission_DenyToolLevel(t *testing.T) {
	_, _, _, _, err := EvaluateToolPermission("deny", "any command", nil)
	if err == nil {
		t.Fatal("want error for deny")
	}
}

// TestEvaluateToolPermission_RuleDeny checks rule deny overrides tool allow.
func TestEvaluateToolPermission_RuleDeny(t *testing.T) {
	rs := &RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf *", Action: "deny", Reason: "no recursive rm"},
	}}

	_, _, _, _, err := EvaluateToolPermission("allow", "rm -rf /", rs)
	if err == nil {
		t.Fatal("want error for rule deny")
	}
}

// TestEvaluateToolPermission_RuleAsk checks rule ask returns ask.
func TestEvaluateToolPermission_RuleAsk(t *testing.T) {
	rs := &RuleSet{Rules: []PermissionRule{
		{Match: "systemctl restart *", Action: "ask", Reason: "restart"},
	}}

	action, reason, pattern, ruleAct, err := EvaluateToolPermission("allow", "systemctl restart nginx", rs)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if action != "ask" {
		t.Errorf("want ask, got %q", action)
	}
	if reason != "restart" {
		t.Errorf("want restart reason, got %q", reason)
	}
	if pattern != "systemctl restart *" {
		t.Errorf("want pattern 'systemctl restart *', got %q", pattern)
	}
	if ruleAct != "ask" {
		t.Errorf("want ruleAct ask, got %q", ruleAct)
	}
}

// TestEvaluateToolPermission_RuleAllow checks rule allow returns allow.
func TestEvaluateToolPermission_RuleAllow(t *testing.T) {
	rs := &RuleSet{Rules: []PermissionRule{
		{Match: "systemctl restart *", Action: "allow"},
	}}

	action, _, _, _, err := EvaluateToolPermission("allow", "systemctl restart nginx", rs)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if action != "allow" {
		t.Errorf("want allow, got %q", action)
	}
}

// TestEvaluateToolPermission_ToolAskNoRule checks tool ask falls through.
func TestEvaluateToolPermission_ToolAskNoRule(t *testing.T) {
	action, _, _, _, err := EvaluateToolPermission("ask", "some command", nil)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if action != "ask" {
		t.Errorf("want ask, got %q", action)
	}
}

// TestEvaluateToolPermission_ToolAskRuleAllow checks rule allow overrides tool ask.
func TestEvaluateToolPermission_ToolAskRuleAllow(t *testing.T) {
	rs := &RuleSet{Rules: []PermissionRule{
		{Match: "some command", Action: "allow"},
	}}

	action, _, _, _, err := EvaluateToolPermission("ask", "some command", rs)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if action != "allow" {
		t.Errorf("want allow, got %q", action)
	}
}

// TestEvaluateToolPermission_NoRuleFallback checks no rule falls through to allow.
func TestEvaluateToolPermission_NoRuleFallback(t *testing.T) {
	rs := &RuleSet{Rules: []PermissionRule{
		{Match: "unrelated", Action: "deny"},
	}}

	action, _, _, _, err := EvaluateToolPermission("allow", "some command", rs)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if action != "allow" {
		t.Errorf("want allow, got %q", action)
	}
}

// TestRules verifies the core permission rule system is functional.
func TestRules(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf *", Action: "deny"},
	}}
	action, _, _ := rs.Evaluate("rm -rf /")
	if action != "deny" {
		t.Errorf("RuleSet.Evaluate: want deny, got %q", action)
	}

	_, _, _, _, err := EvaluateToolPermission("deny", "anything", nil)
	if err == nil {
		t.Error("EvaluateToolPermission: want error for tool-level deny")
	}

	merged := MergeRules([]string{"destructive"}, []PermissionRule{
		{Match: "custom-rule", Action: "allow"},
	})
	if len(merged.Rules) == 0 {
		t.Fatal("MergeRules: returned empty set")
	}
	if merged.Rules[0].Match != "custom-rule" {
		t.Errorf("MergeRules first rule: want 'custom-rule', got %q",
			merged.Rules[0].Match)
	}
}

// --- Normalization bypass tests ---

// TestRuleSet_Evaluate_ExtraWhitespace_Blocked verifies that extra
// whitespace in the command does not evade a deny rule.
func TestRuleSet_Evaluate_ExtraWhitespace_Blocked(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf *", Action: "deny", Reason: "destructive"},
	}}

	// Double space between rm and -rf should still match.
	action, reason, _ := rs.Evaluate("rm  -rf /")
	if action != "deny" {
		t.Errorf("want deny for double-space command, got %q", action)
	}
	if reason != "destructive" {
		t.Errorf("want 'destructive' reason, got %q", reason)
	}
}

// TestRuleSet_Evaluate_AbsolutePath_Blocked verifies that an absolute
// path prefix (/usr/bin/) does not evade a deny rule.
func TestRuleSet_Evaluate_AbsolutePath_Blocked(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf *", Action: "deny"},
	}}

	action, _, _ := rs.Evaluate("/usr/bin/rm -rf /")
	if action != "deny" {
		t.Errorf("want deny for /usr/bin/rm, got %q", action)
	}
}

// TestRuleSet_Evaluate_BinPath_Blocked verifies /bin/ prefix is
// stripped during normalization.
func TestRuleSet_Evaluate_BinPath_Blocked(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf *", Action: "deny"},
	}}

	action, _, _ := rs.Evaluate("/bin/rm -rf /")
	if action != "deny" {
		t.Errorf("want deny for /bin/rm, got %q", action)
	}
}

// TestRuleSet_Evaluate_EnvPrefix_Blocked verifies that an "env " prefix
// does not evade a deny rule.
func TestRuleSet_Evaluate_EnvPrefix_Blocked(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf *", Action: "deny"},
	}}

	action, _, _ := rs.Evaluate("env rm -rf /")
	if action != "deny" {
		t.Errorf("want deny for 'env rm -rf /', got %q", action)
	}
}

// TestRuleSet_Evaluate_ArgumentSplit_Blocked verifies that split
// arguments (-r -f instead of -rf) still match the wildcard pattern.
func TestRuleSet_Evaluate_ArgumentSplit_Blocked(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm *", Action: "deny"},
	}}

	action, _, _ := rs.Evaluate("rm -r -f /")
	if action != "deny" {
		t.Errorf("want deny for 'rm -r -f /', got %q", action)
	}
}

// TestNormalizeCommand_CollapseWhitespace verifies whitespace collapse.
func TestNormalizeCommand_CollapseWhitespace(t *testing.T) {
	got := normalizeCommand("  rm   -rf   /  ")
	want := "rm -rf /"
	if got != want {
		t.Errorf("normalizeCommand: want %q, got %q", want, got)
	}
}

// TestNormalizeCommand_StripEnv verifies env prefix removal.
func TestNormalizeCommand_StripEnv(t *testing.T) {
	got := normalizeCommand("env rm -rf /")
	want := "rm -rf /"
	if got != want {
		t.Errorf("normalizeCommand: want %q, got %q", want, got)
	}
}

// TestNormalizeCommand_StripAbsPath verifies absolute path prefix removal.
func TestNormalizeCommand_StripAbsPath(t *testing.T) {
	got := normalizeCommand("/usr/bin/rm -rf /")
	want := "rm -rf /"
	if got != want {
		t.Errorf("normalizeCommand: want %q, got %q", want, got)
	}
}
