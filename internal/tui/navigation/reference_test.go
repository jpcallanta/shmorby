package navigation

import (
	"testing"
)

func TestReferenceEngine_Empty(t *testing.T) {
	e := NewReferenceEngine()
	results := e.Complete("")
	if len(results) != 0 {
		t.Errorf("want 0 results, got %d", len(results))
	}
}

func TestReferenceEngine_AddSource(t *testing.T) {
	e := NewReferenceEngine()
	e.AddSource(ReferenceSource{
		Alias: "web",
		Path:  "/var/www",
		Items: []RefItem{
			{Label: "server1", Value: "server1.example.com", Kind: "host"},
			{Label: "config", Value: "/var/www/config.json", Kind: "file"},
		},
	})
	results := e.Complete("")
	if len(results) != 3 {
		t.Fatalf("want 3 results (source + 2 items), got %d", len(results))
	}
}

func TestReferenceEngine_FuzzyMatch(t *testing.T) {
	e := NewReferenceEngine()
	e.AddSource(ReferenceSource{
		Alias: "db",
		Items: []RefItem{
			{Label: "postgres-primary", Value: "pg1.example.com", Kind: "host"},
			{Label: "redis-cache", Value: "redis.example.com", Kind: "host"},
		},
	})
	// Prefix match
	results := e.Complete("db")
	if len(results) != 1 {
		t.Fatalf("want 1 match for 'db', got %d", len(results))
	}
	// Substring match
	results = e.Complete("postgres")
	if len(results) != 1 {
		t.Fatalf("want 1 match for 'postgres', got %d", len(results))
	}
	// No match
	results = e.Complete("nonexistent")
	if len(results) != 0 {
		t.Errorf("want 0 matches, got %d", len(results))
	}
}

func TestReferenceEngine_Resolve(t *testing.T) {
	e := NewReferenceEngine()
	e.AddSource(ReferenceSource{
		Alias: "web",
		Path:  "/var/www",
		Items: []RefItem{
			{Label: "server1", Value: "server1.example.com", Kind: "host"},
		},
	})
	content, kind, err := e.Resolve("web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "/var/www" {
		t.Errorf("want /var/www, got %q", content)
	}
	if kind != "file" {
		t.Errorf("want kind file, got %q", kind)
	}
	content, kind, err = e.Resolve("server1.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != "host" {
		t.Errorf("want kind host, got %q", kind)
	}
	if content != "server1.example.com" {
		t.Errorf("want server1.example.com, got %q", content)
	}
}

func TestReferenceEngine_Resolve_NotFound(t *testing.T) {
	e := NewReferenceEngine()
	content, kind, err := e.Resolve("unknown")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if content != "" {
		t.Errorf("want empty content, got %q", content)
	}
	if kind != "" {
		t.Errorf("want empty kind, got %q", kind)
	}
}

func TestReferenceEngine_Sources(t *testing.T) {
	e := NewReferenceEngine()
	e.AddSource(ReferenceSource{Alias: "test"})
	sources := e.Sources()
	if len(sources) != 1 {
		t.Fatalf("want 1 source, got %d", len(sources))
	}
}
