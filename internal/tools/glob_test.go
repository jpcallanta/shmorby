package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFindTool_Basic checks finding files matching a glob pattern.
func TestFindTool_Basic(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	c := filepath.Join(dir, "c.md")

	for _, f := range []string{a, b, c} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := NewFindTool("allow")
	args, _ := json.Marshal(findArgs{Path: dir, Pattern: "*.go"})

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Errorf("want a.go and b.go in output, got %q", out)
	}
	if strings.Contains(out, "c.md") {
		t.Errorf("unwanted c.md in output, got %q", out)
	}
}

// TestFindTool_MaxDepth checks depth limit.
func TestFindTool_MaxDepth(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(nested, "x.go")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewFindTool("allow")
	args, _ := json.Marshal(findArgs{Path: dir, Pattern: "*.go", MaxDepth: 1})

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out, "x.go") {
		t.Errorf("x.go should be beyond max depth, got %q", out)
	}
}

// TestFindTool_TypeFilter checks filtering by file vs dir.
func TestFindTool_TypeFilter(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "mydir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "myfile.txt")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewFindTool("allow")

	t.Run("files only", func(t *testing.T) {
		args, _ := json.Marshal(findArgs{Path: dir, Pattern: "my*", Type: "file"})
		out, err := tool.Run(context.Background(), args)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !strings.Contains(out, "myfile.txt") {
			t.Errorf("want myfile.txt, got %q", out)
		}
		if strings.Contains(out, "mydir") {
			t.Errorf("unwanted mydir in file results, got %q", out)
		}
	})

	t.Run("dirs only", func(t *testing.T) {
		args, _ := json.Marshal(findArgs{Path: dir, Pattern: "my*", Type: "dir"})
		out, err := tool.Run(context.Background(), args)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !strings.Contains(out, "mydir") {
			t.Errorf("want mydir, got %q", out)
		}
		if strings.Contains(out, "myfile.txt") {
			t.Errorf("unwanted myfile.txt in dir results, got %q", out)
		}
	})
}

// TestFindTool_Cancel checks early return on context cancel.
func TestFindTool_Cancel(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 100; i++ {
		sub := filepath.Join(dir, "d"+string(rune('0'+i%10)), "sub")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "f.go"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := NewFindTool("allow")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	args, _ := json.Marshal(findArgs{Path: dir, Pattern: "*.go"})
	_, err := tool.Run(ctx, args)
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
}

// TestFindTool_UnreadableDir checks graceful skip of permission-denied dirs.
func TestFindTool_UnreadableDir(t *testing.T) {
	tmp := t.TempDir()
	okDir := filepath.Join(tmp, "ok")
	if err := os.MkdirAll(okDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(okDir, "f.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	noPerm := filepath.Join(tmp, "noperm")
	if err := os.MkdirAll(noPerm, 0o000); err != nil {
		t.Fatal(err)
	}

	tool := NewFindTool("allow")
	args, _ := json.Marshal(findArgs{Path: tmp, Pattern: "*.txt"})

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "f.txt") {
		t.Errorf("want f.txt in output, got %q", out)
	}
}

// TestFindTool_MaxResults checks capping at 500 results.
func TestFindTool_MaxResults(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 600; i++ {
		if err := os.WriteFile(
			filepath.Join(dir, fmt.Sprintf("f_%d.go", i)),
			nil, 0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	tool := NewFindTool("allow")
	args, _ := json.Marshal(findArgs{Path: dir, Pattern: "*.go"})

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 500 {
		t.Errorf("want <= 500 results, got %d", len(lines))
	}
}

// TestFindTool_NoMatches returns no matches message.
func TestFindTool_NoMatches(t *testing.T) {
	dir := t.TempDir()
	tool := NewFindTool("allow")
	args, _ := json.Marshal(findArgs{Path: dir, Pattern: "*.nonexistent"})

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "no matches" {
		t.Errorf("want 'no matches', got %q", out)
	}
}

// TestFindTool_InvalidJSON returns error.
func TestFindTool_InvalidJSON(t *testing.T) {
	tool := NewFindTool("allow")
	_, err := tool.Run(context.Background(), []byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}

// TestFindTool_MissingPattern returns error.
func TestFindTool_MissingPattern(t *testing.T) {
	tool := NewFindTool("allow")
	_, err := tool.Run(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("want error for missing pattern, got nil")
	}
}

// TestFindTool_DefaultPath uses current directory.
func TestFindTool_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewFindTool("allow")
	args, _ := json.Marshal(findArgs{Pattern: "*.go"})

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "hello.go") {
		t.Errorf("want hello.go in output, got %q", out)
	}
}

// TestFindTool_ContextCancelledDuringWalk checks context cancellation
// during an active walk.
func TestFindTool_ContextCancelledDuringWalk(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2000; i++ {
		if err := os.WriteFile(
			filepath.Join(dir, fmt.Sprintf("f_%d.go", i)),
			nil, 0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	tool := NewFindTool("allow")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	time.Sleep(2 * time.Millisecond)

	args, _ := json.Marshal(findArgs{Path: dir, Pattern: "*.go"})
	_, err := tool.Run(ctx, args)
	if err == nil {
		t.Fatal("want context deadline error, got nil")
	}
}

// TestFindTool_NameDescriptionPermLevel checks basic metadata.
func TestFindTool_NameDescriptionPermLevel(t *testing.T) {
	tool := NewFindTool("ask")
	if tool.Name() != "find" {
		t.Errorf("Name = %q, want %q", tool.Name(), "find")
	}
	if tool.Description() == "" {
		t.Error("Description is empty")
	}
	if tool.PermLevel() != "ask" {
		t.Errorf("PermLevel = %q, want %q", tool.PermLevel(), "ask")
	}
	if tool.Parameters() == nil {
		t.Error("Parameters is nil")
	}
}

// TestFindTool_SetPerm updates perm level.
func TestFindTool_SetPerm(t *testing.T) {
	tool := NewFindTool("allow")
	tool.SetPerm("deny")
	if tool.PermLevel() != "deny" {
		t.Errorf("PermLevel = %q, want %q", tool.PermLevel(), "deny")
	}
}
