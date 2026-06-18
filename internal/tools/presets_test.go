package tools

import "testing"

// TestPresets_NotEmpty checks all defined presets have rules.
func TestPresets_NotEmpty(t *testing.T) {
	if len(Presets) == 0 {
		t.Fatal("Presets map is empty")
	}
	for name, rules := range Presets {
		if len(rules) == 0 {
			t.Errorf("preset %q has no rules", name)
		}
	}
}

// TestPresets_Destructive checks destructive preset rules.
func TestPresets_Destructive(t *testing.T) {
	rules, ok := Presets["destructive"]
	if !ok {
		t.Fatal("destructive preset not found")
	}
	if len(rules) == 0 {
		t.Fatal("destructive preset is empty")
	}
}

// TestPresets_Service checks service preset rules.
func TestPresets_Service(t *testing.T) {
	rules, ok := Presets["service"]
	if !ok {
		t.Fatal("service preset not found")
	}
	found := false
	for _, r := range rules {
		if r.Match == "systemctl restart *" {
			found = true
			if r.Action != "ask" {
				t.Errorf("restart action: want ask, got %q", r.Action)
			}
			if r.Reason != "service restart" {
				t.Errorf("restart reason: want 'service restart', got %q", r.Reason)
			}
		}
	}
	if !found {
		t.Error("systemctl restart * not found in service preset")
	}
}

// TestPresets_Package checks package preset rules.
func TestPresets_Package(t *testing.T) {
	rules, ok := Presets["package"]
	if !ok {
		t.Fatal("package preset not found")
	}
	if len(rules) == 0 {
		t.Fatal("package preset is empty")
	}
}

// TestPresets_SSH checks ssh preset allows all.
func TestPresets_SSH(t *testing.T) {
	rules, ok := Presets["ssh"]
	if !ok {
		t.Fatal("ssh preset not found")
	}
	for _, r := range rules {
		if r.Action != "allow" {
			t.Errorf("ssh rule %q: want allow, got %q", r.Match, r.Action)
		}
	}
}

// TestPresets_Sudo checks sudo preset rules exist.
func TestPresets_Sudo(t *testing.T) {
	rules, ok := Presets["sudo"]
	if !ok {
		t.Fatal("sudo preset not found")
	}
	if len(rules) == 0 {
		t.Fatal("sudo preset is empty")
	}
}

// TestMergeRules_MergesPresetsInOrder checks merge ordering.
func TestMergeRules_MergesPresetsInOrder(t *testing.T) {
	rs := MergeRules([]string{"destructive", "service"}, nil)
	if len(rs.Rules) == 0 {
		t.Fatal("MergeRules returned empty set")
	}
	// First few rules should be from destructive preset.
	if rs.Rules[0].Match != "rm -rf *" {
		t.Errorf("first rule: want 'rm -rf *', got %q", rs.Rules[0].Match)
	}
	// Last of destructive, first of service boundary check.
	foundDestructiveEnd := false
	foundServiceStart := false
	for _, r := range rs.Rules {
		if r.Match == "shred *" {
			foundDestructiveEnd = true
		}
		if r.Match == "systemctl start *" {
			foundServiceStart = true
		}
	}
	if !foundDestructiveEnd {
		t.Error("destructive 'shred *' not found in merged rules")
	}
	if !foundServiceStart {
		t.Error("service 'systemctl start *' not found in merged rules")
	}
}

// TestMergeRules_CustomPrecedesPreset checks custom rules come before
// presets so they win via first-match-wins.
func TestMergeRules_CustomPrecedesPreset(t *testing.T) {
	custom := []PermissionRule{
		{Match: "my-custom-command", Action: "deny", Reason: "custom"},
	}
	rs := MergeRules([]string{"destructive"}, custom)
	if len(rs.Rules) == 0 {
		t.Fatal("MergeRules returned empty set")
	}
	first := rs.Rules[0]
	if first.Match != "my-custom-command" {
		t.Errorf("first rule: want 'my-custom-command', got %q", first.Match)
	}
}

// TestMergeRules_UnknownPresetIsSkipped checks unknown preset name does
// not cause error.
func TestMergeRules_UnknownPresetIsSkipped(t *testing.T) {
	rs := MergeRules([]string{"nonexistent-preset"}, nil)
	if len(rs.Rules) != 0 {
		t.Errorf("want empty rules, got %d", len(rs.Rules))
	}
}

// TestMergeRules_NoPresetsOnlyCustom checks only custom rules are merged.
func TestMergeRules_NoPresetsOnlyCustom(t *testing.T) {
	custom := []PermissionRule{
		{Match: "cmd", Action: "allow"},
	}
	rs := MergeRules(nil, custom)
	if len(rs.Rules) != 1 {
		t.Errorf("want 1 rule, got %d", len(rs.Rules))
	}
}

// TestEvaluateWithMergedRules checks merged rule set evaluates correctly.
func TestEvaluateWithMergedRules(t *testing.T) {
	rs := MergeRules([]string{"service"}, nil)

	action, reason := rs.Evaluate("systemctl restart nginx")
	if action != "ask" {
		t.Errorf("want ask, got %q", action)
	}
	if reason != "service restart" {
		t.Errorf("want 'service restart', got %q", reason)
	}
}
