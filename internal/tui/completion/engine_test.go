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
	if len(matches) != 9 {
		t.Errorf("want 9 commands, got %d", len(matches))
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
	if len(matches) != 1 {
		t.Fatalf("want 1 match (/agent), got %d", len(matches))
	}
	if matches[0].Name != "/agent" {
		t.Errorf("want /agent, got %s", matches[0].Name)
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
	if len(all) != 9 {
		t.Errorf("want 9 commands, got %d", len(all))
	}
}
