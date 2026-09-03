package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shmorby/internal/ledger"
)

// TestLedgerGetTool_Name verifies tool name.
func TestLedgerGetTool_Name(t *testing.T) {

	tool := NewLedgerGetTool("allow")
	if got := tool.Name(); got != "ledger_get" {
		t.Errorf("Name: want ledger_get, got %q", got)
	}
}

// TestLedgerGetTool_PermLevel verifies permission level.
func TestLedgerGetTool_PermLevel(t *testing.T) {

	tool := NewLedgerGetTool("ask")
	if got := tool.PermLevel(); got != "ask" {
		t.Errorf("PermLevel: want ask, got %q", got)
	}
}

// TestLedgerGetTool_SetPerm verifies runtime perm change.
func TestLedgerGetTool_SetPerm(t *testing.T) {

	tool := NewLedgerGetTool("allow")
	tool.SetPerm("deny")
	if got := tool.PermLevel(); got != "deny" {
		t.Errorf("SetPerm: want deny, got %q", got)
	}
}

// TestLedgerGetTool_InvalidArgs verifies error on bad args.
func TestLedgerGetTool_InvalidArgs(t *testing.T) {

	tool := NewLedgerGetTool("allow")
	_, err := tool.Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Error("want error for missing section, got nil")
	}
}

// TestLedgerGetTool_InvalidSection verifies error on bad section name.
func TestLedgerGetTool_InvalidSection(t *testing.T) {

	tool := NewLedgerGetTool("allow")
	_, err := tool.Run(
		context.Background(),
		json.RawMessage(`{"section":"bad/name"}`),
	)
	if err == nil {
		t.Error("want error for invalid section, got nil")
	}
}

// TestLedgerSetTool_Name verifies tool name.
func TestLedgerSetTool_Name(t *testing.T) {

	tool := NewLedgerSetTool("ask")
	if got := tool.Name(); got != "ledger_set" {
		t.Errorf("Name: want ledger_set, got %q", got)
	}
}

// TestLedgerSetTool_PermLevel verifies permission level.
func TestLedgerSetTool_PermLevel(t *testing.T) {

	tool := NewLedgerSetTool("allow")
	if got := tool.PermLevel(); got != "allow" {
		t.Errorf("PermLevel: want allow, got %q", got)
	}
}

// TestLedgerSetTool_InvalidArgs verifies error on bad args.
func TestLedgerSetTool_InvalidArgs(t *testing.T) {

	tool := NewLedgerSetTool("allow")
	_, err := tool.Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Error("want error for missing fields, got nil")
	}
}

// TestLedgerSetTool_InvalidJSON verifies error on invalid JSON data.
func TestLedgerSetTool_InvalidJSON(t *testing.T) {

	tool := NewLedgerSetTool("allow")
	_, err := tool.Run(
		context.Background(),
		json.RawMessage(`{"section":"hosts","data":not-json}`),
	)
	if err == nil {
		t.Error("want error for invalid JSON, got nil")
	}
}

// TestLedgerSetTool_Redaction verifies secrets are redacted before storage.
func TestLedgerSetTool_Redaction(t *testing.T) {

	// Set up a temp ledger directory.
	// UserDataDir() is XDG_DATA_HOME/shmorby on Unix and
	// LOCALAPPDATA/shmorby on Windows; set both for cross-platform.
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "shmorby")
	t.Setenv("XDG_DATA_HOME", baseDir)
	t.Setenv("LOCALAPPDATA", baseDir)

	// Create ledger at the data dir.
	l, err := ledger.OpenAt(dataDir)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	l.Close()

	tool := NewLedgerSetTool("allow")

	// Payload with a fake AWS key.
	payload := `{"section":"creds","data":{"key":"AKIA1234567890ABCDEF"}}`
	result, err := tool.Run(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result, "written") {
		t.Errorf("want 'written' in result, got %q", result)
	}

	// Read the ledger and verify the key was redacted.
	l2, err := ledger.OpenAt(dataDir)
	if err != nil {
		t.Fatalf("reopen ledger: %v", err)
	}
	defer l2.Close()

	data, ok := l2.Get("creds")
	if !ok {
		t.Fatal("creds section not found")
	}

	// The AKIA key should be redacted.
	if strings.Contains(string(data), "AKIA1234567890ABCDEF") {
		t.Errorf("secret not redacted in ledger: %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got: %s", data)
	}

	// Verify the encrypted file doesn't contain the plaintext.
	encFile := filepath.Join(dataDir, "ledger.json.age")
	encBytes, err := os.ReadFile(encFile)
	if err != nil {
		t.Fatalf("read encrypted file: %v", err)
	}
	if strings.Contains(string(encBytes), "AKIA1234567890ABCDEF") {
		t.Error("plaintext secret found in encrypted file")
	}
}

// TestLedgerSetTool_SizeCap verifies oversized payloads are rejected.
func TestLedgerSetTool_SizeCap(t *testing.T) {

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "shmorby")
	t.Setenv("XDG_DATA_HOME", baseDir)
	t.Setenv("LOCALAPPDATA", baseDir)

	l, err := ledger.OpenAt(dataDir)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	l.Close()

	tool := NewLedgerSetTool("allow")

	// Create a payload over the size cap.
	largeData := make([]byte, ledger.MaxSectionBytes+100)
	for i := range largeData {
		largeData[i] = 'x'
	}
	payload := `{"section":"big","data":"` + string(largeData) + `"}`

	_, err = tool.Run(context.Background(), json.RawMessage(payload))
	if err == nil {
		t.Error("want error for oversized payload, got nil")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("error should mention cap: %v", err)
	}
}

// TestLedgerGetSet_RoundTrip verifies get after set returns the data.
func TestLedgerGetSet_RoundTrip(t *testing.T) {

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "shmorby")
	t.Setenv("XDG_DATA_HOME", baseDir)
	t.Setenv("LOCALAPPDATA", baseDir)

	l, err := ledger.OpenAt(dataDir)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	l.Close()

	setTool := NewLedgerSetTool("allow")
	getTool := NewLedgerGetTool("allow")

	// Set a section.
	setPayload := `{"section":"hosts","data":{"web1":"nginx","db1":"postgres"}}`
	_, err = setTool.Run(context.Background(), json.RawMessage(setPayload))
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	// Get the section.
	getPayload := `{"section":"hosts"}`
	result, err := getTool.Run(context.Background(), json.RawMessage(getPayload))
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !strings.Contains(result, "web1") {
		t.Errorf("result should contain web1: %s", result)
	}
	if !strings.Contains(result, "nginx") {
		t.Errorf("result should contain nginx: %s", result)
	}
}

// TestLedgerSetTool_JSONKeyRedaction verifies JSON object keys like
// password/api_key are redacted (not just embedded patterns).
func TestLedgerSetTool_JSONKeyRedaction(t *testing.T) {

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "shmorby")
	t.Setenv("XDG_DATA_HOME", baseDir)
	t.Setenv("LOCALAPPDATA", baseDir)

	l, err := ledger.OpenAt(dataDir)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	l.Close()

	tool := NewLedgerSetTool("allow")

	payload := `{"section":"creds","data":` +
		`{"password":"hunter2","api_key":"sk-12345678901234567890123456789012",` +
		`"host":"web1"}}`
	result, err := tool.Run(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result, "written") {
		t.Errorf("want 'written' in result, got %q", result)
	}

	// Read the ledger and verify JSON-key secrets were redacted
	// while non-secret data and valid JSON structure were kept.
	l2, err := ledger.OpenAt(dataDir)
	if err != nil {
		t.Fatalf("reopen ledger: %v", err)
	}
	defer l2.Close()

	data, ok := l2.Get("creds")
	if !ok {
		t.Fatal("creds section not found")
	}
	for _, secret := range []string{"hunter2", "sk-12345678901234567890123456789012"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("secret %q stored in ledger: %s", secret, data)
		}
	}
	if !json.Valid(data) {
		t.Errorf("stored data is not valid JSON: %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got: %s", data)
	}
	if !strings.Contains(string(data), "web1") {
		t.Errorf("non-secret data lost: %s", data)
	}

	// Verify the encrypted file doesn't contain the plaintext.
	encFile := filepath.Join(dataDir, "ledger.json.age")
	encBytes, err := os.ReadFile(encFile)
	if err != nil {
		t.Fatalf("read encrypted file: %v", err)
	}
	if strings.Contains(string(encBytes), "hunter2") {
		t.Error("plaintext secret found in encrypted file")
	}
}

// TestLedgerGetTool_SectionNotFound verifies error message for missing section.
func TestLedgerGetTool_SectionNotFound(t *testing.T) {

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "shmorby")
	t.Setenv("XDG_DATA_HOME", baseDir)
	t.Setenv("LOCALAPPDATA", baseDir)

	l, err := ledger.OpenAt(dataDir)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	l.Close()

	tool := NewLedgerGetTool("allow")
	result, err := tool.Run(
		context.Background(),
		json.RawMessage(`{"section":"nonexistent"}`),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("want 'not found' in result, got %q", result)
	}
}
