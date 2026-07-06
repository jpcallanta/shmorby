package completion

import (
	"testing"
)

func TestComplete_NoPrefix(t *testing.T) {
	e := New()
	matches := e.Complete("hello")
	if matches != nil {
		t.Errorf("want nil, got %d matches", len(matches))
	}
}

func TestComplete_Slash(t *testing.T) {
	e := New()
	matches := e.Complete("/")
	if len(matches) != 11 {
		t.Errorf("want 11 commands, got %d", len(matches))
	}
}

func TestComplete_Narrow(t *testing.T) {
	e := New()
	matches := e.Complete("/q")
	if len(matches) != 1 {
		t.Fatalf("want 1 match, got %d", len(matches))
	}
	if matches[0].Name != "/quit" {
		t.Errorf("want /quit, got %s", matches[0].Name)
	}
}

func TestComplete_CaseInsensitive(t *testing.T) {
	e := New()
	matches := e.Complete("/QUIT")
	if len(matches) != 1 {
		t.Fatalf("want 1 match, got %d", len(matches))
	}
	if matches[0].Name != "/quit" {
		t.Errorf("want /quit, got %s", matches[0].Name)
	}
}

func TestComplete_AgentPrefix(t *testing.T) {
	e := New()
	matches := e.Complete("/a")
	if len(matches) != 2 {
		t.Fatalf("want 2 matches (/agent, /apikey), got %d",
			len(matches))
	}
	found := make(map[string]bool)
	for _, m := range matches {
		found[m.Name] = true
	}
	if !found["/agent"] {
		t.Error("want /agent in matches")
	}
	if !found["/apikey"] {
		t.Error("want /apikey in matches")
	}
}

func TestComplete_NoMatch(t *testing.T) {
	e := New()
	matches := e.Complete("/zzz")
	if len(matches) != 0 {
		t.Errorf("want 0 matches, got %d", len(matches))
	}
}

func TestAll(t *testing.T) {
	e := New()
	all := e.All()
	if len(all) != 11 {
		t.Errorf("want 11 commands, got %d", len(all))
	}
}
