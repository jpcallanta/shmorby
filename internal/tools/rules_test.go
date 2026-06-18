package tools

import "testing"

// TestRuleSet_Evaluate_ExactMatch checks exact command matching.
func TestRuleSet_Evaluate_ExactMatch(t *testing.T) {
	rs := RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf /", Action: "deny", Reason: "root destruction"},
	}}

	action, reason := rs.Evaluate("rm -rf /")
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

	action, reason := rs.Evaluate("systemctl restart nginx")
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

	action, _ := rs.Evaluate("aws ec2 describe-instances")
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

	action, reason := rs.Evaluate("rm -rf /")
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

	action, _ := rs.Evaluate("ls -la")
	if action != "" {
		t.Errorf("want empty, got %q", action)
	}
}

// TestRuleSet_Evaluate_EmptyRules checks empty rule set returns empty.
func TestRuleSet_Evaluate_EmptyRules(t *testing.T) {
	rs := RuleSet{}

	action, _ := rs.Evaluate("anything")
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
	_, _, err := EvaluateToolPermission("deny", "any command", nil)
	if err == nil {
		t.Fatal("want error for deny")
	}
}

// TestEvaluateToolPermission_RuleDeny checks rule deny overrides tool allow.
func TestEvaluateToolPermission_RuleDeny(t *testing.T) {
	rs := &RuleSet{Rules: []PermissionRule{
		{Match: "rm -rf *", Action: "deny", Reason: "no recursive rm"},
	}}

	_, _, err := EvaluateToolPermission("allow", "rm -rf /", rs)
	if err == nil {
		t.Fatal("want error for rule deny")
	}
}

// TestEvaluateToolPermission_RuleAsk checks rule ask returns ask.
func TestEvaluateToolPermission_RuleAsk(t *testing.T) {
	rs := &RuleSet{Rules: []PermissionRule{
		{Match: "systemctl restart *", Action: "ask", Reason: "restart"},
	}}

	action, reason, err := EvaluateToolPermission("allow", "systemctl restart nginx", rs)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if action != "ask" {
		t.Errorf("want ask, got %q", action)
	}
	if reason != "restart" {
		t.Errorf("want restart reason, got %q", reason)
	}
}

// TestEvaluateToolPermission_RuleAllow checks rule allow returns allow.
func TestEvaluateToolPermission_RuleAllow(t *testing.T) {
	rs := &RuleSet{Rules: []PermissionRule{
		{Match: "systemctl restart *", Action: "allow"},
	}}

	action, _, err := EvaluateToolPermission("allow", "systemctl restart nginx", rs)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if action != "allow" {
		t.Errorf("want allow, got %q", action)
	}
}

// TestEvaluateToolPermission_ToolAskNoRule checks tool ask falls through.
func TestEvaluateToolPermission_ToolAskNoRule(t *testing.T) {
	action, _, err := EvaluateToolPermission("ask", "some command", nil)
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

	action, _, err := EvaluateToolPermission("ask", "some command", rs)
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

	action, _, err := EvaluateToolPermission("allow", "some command", rs)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if action != "allow" {
		t.Errorf("want allow, got %q", action)
	}
}
