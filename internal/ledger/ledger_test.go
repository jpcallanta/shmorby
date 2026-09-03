package ledger

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filippo.io/age"
)

// TestOpenAt_NewLedger_CreatesKeyAndData verifies first-use
// behaviour: key generation, empty data, no error.
func TestOpenAt_NewLedger_CreatesKeyAndData(t *testing.T) {

	dir := t.TempDir()

	l, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("open: want nil, got %v", err)
	}

	if len(l.Sections()) != 0 {

		t.Errorf("sections: want 0, got %d", len(l.Sections()))
	}

	// Key file must exist with restrictive perms.
	keyPath := filepath.Join(dir, keyFile)
	info, err := os.Stat(keyPath)
	if err != nil {

		t.Fatalf("stat key: %v", err)
	}

	// On Windows perms are best-effort (inherited ACL);
	// only enforce 0600 on Unix.
	if runtime.GOOS != "windows" {

		if perm := info.Mode().Perm(); perm != 0o600 {

			t.Errorf("key perm: want 0600, got %o", perm)
		}
	}

	if err := l.Close(); err != nil {

		t.Fatalf("close: %v", err)
	}
}

// TestSetGet_RoundTrip_PersistsData verifies set+close+open
// survives a process restart.
func TestSetGet_RoundTrip_PersistsData(t *testing.T) {

	dir := t.TempDir()

	payload := json.RawMessage(`{"host":"web1","role":"nginx"}`)

	l, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("open: %v", err)
	}

	l.Set("hosts", payload)

	if err := l.Close(); err != nil {

		t.Fatalf("close: %v", err)
	}

	// Re-open and verify.
	l2, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("reopen: %v", err)
	}
	defer l2.Close()

	got, ok := l2.Get("hosts")
	if !ok {

		t.Fatalf("get hosts: want true, got false")
	}

	// Compare parsed JSON, not raw strings (MarshalIndent reformats).
	var gotVal, wantVal interface{}
	if err := json.Unmarshal(got, &gotVal); err != nil {

		t.Fatalf("unmarshal got: %v", err)
	}

	if err := json.Unmarshal(payload, &wantVal); err != nil {

		t.Fatalf("unmarshal want: %v", err)
	}

	gotJSON, _ := json.Marshal(gotVal)
	wantJSON, _ := json.Marshal(wantVal)

	if string(gotJSON) != string(wantJSON) {

		t.Errorf("hosts: want %s, got %s", wantJSON, gotJSON)
	}
}

// TestDelete_RemovesSection verifies deletion.
func TestDelete_RemovesSection(t *testing.T) {

	dir := t.TempDir()

	l, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	l.Set("hosts", json.RawMessage(`[]`))
	l.Set("tickets", json.RawMessage(`[]`))

	l.Delete("hosts")

	if _, ok := l.Get("hosts"); ok {

		t.Errorf("hosts after delete: want gone, got present")
	}

	if _, ok := l.Get("tickets"); !ok {

		t.Errorf("tickets: want present, got gone")
	}
}

// TestSections_ReturnsSorted verifies sorted section listing.
func TestSections_ReturnsSorted(t *testing.T) {

	dir := t.TempDir()

	l, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	l.Set("tickets", json.RawMessage(`[]`))
	l.Set("hosts", json.RawMessage(`[]`))
	l.Set("incidents", json.RawMessage(`[]`))

	got := l.Sections()
	want := []string{"hosts", "incidents", "tickets"}

	if len(got) != len(want) {

		t.Fatalf("sections len: want %d, got %d", len(want), len(got))
	}

	for i := range want {

		if got[i] != want[i] {

			t.Errorf("sections[%d]: want %q, got %q", i, want[i], got[i])
		}
	}
}

// Verifies the data file is not plaintext.
func TestEncrypted_AtRest(t *testing.T) {

	dir := t.TempDir()

	l, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("open: %v", err)
	}

	l.Set("hosts", json.RawMessage(`{"secret":"hunter2"}`))

	if err := l.Close(); err != nil {

		t.Fatalf("close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, dataFile))
	if err != nil {

		t.Fatalf("read data file: %v", err)
	}

	// Must NOT contain the plaintext payload.
	if strings.Contains(string(raw), "hunter2") {

		t.Errorf("data file contains plaintext secret")
	}

	if strings.Contains(string(raw), "hosts") {

		t.Errorf("data file contains plaintext section name")
	}
}

// TestConcurrentLock_SerialisesAccess verifies that two Open
// calls on the same dir block correctly (second one waits for
// first to close).
func TestConcurrentLock_SerialisesAccess(t *testing.T) {

	dir := t.TempDir()

	l1, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("open 1: %v", err)
	}

	// Second open should block until l1 is closed. Use a
	// channel to detect completion from a goroutine.
	done := make(chan error, 1)
	go func() {

		l2, err := OpenAt(dir)
		if err != nil {

			done <- err

			return
		}

		done <- l2.Close()
	}()

	// l2 should be blocked here.
	l1.Set("test", json.RawMessage(`"value"`))

	if err := l1.Close(); err != nil {

		t.Fatalf("close 1: %v", err)
	}

	// Now l2 should succeed.
	if err := <-done; err != nil {

		t.Fatalf("open/close 2: %v", err)
	}

	// Verify data persisted from l1.
	l3, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("open 3: %v", err)
	}
	defer l3.Close()

	got, ok := l3.Get("test")
	if !ok {

		t.Fatalf("get test: want true, got false")
	}

	if string(got) != `"value"` {

		t.Errorf("test: want %q, got %q", `"value"`, got)
	}
}

// TestValidateSection_Cases verifies section name validation.
func TestValidateSection_Cases(t *testing.T) {

	cases := []struct {
		name    string
		wantErr bool
	}{
		{"hosts", false},
		{"my-section", false},
		{"section_1", false},
		{"", true},
		{"bad/name", true},
		{"bad.name", true},
		{"bad name", true},
		{"bad\\name", true},
	}

	for _, tc := range cases {

		err := ValidateSection(tc.name)
		if tc.wantErr && err == nil {

			t.Errorf("ValidateSection(%q): want error, got nil", tc.name)
		}

		if !tc.wantErr && err != nil {

			t.Errorf("ValidateSection(%q): want nil, got %v", tc.name, err)
		}
	}
}

// TestConcurrentFirstUse_NoKeyRace verifies that concurrent
// first-use Opens on a fresh directory all converge on the same
// key (C1 fix: lock acquired before key generation).
func TestConcurrentFirstUse_NoKeyRace(t *testing.T) {

	dir := t.TempDir()

	const n = 4
	errs := make(chan error, n)

	for i := 0; i < n; i++ {

		go func() {

			l, err := OpenAt(dir)
			if err != nil {

				errs <- err

				return
			}

			// Write a unique section so we can verify
			// all opens used the same key.
			l.Set("race-test", json.RawMessage(`"ok"`))
			errs <- l.Close()
		}()
	}

	for i := 0; i < n; i++ {

		if err := <-errs; err != nil {

			t.Errorf("concurrent open %d: %v", i, err)
		}
	}

	// Verify data survived — all writers used the same key.
	l, err := OpenAt(dir)
	if err != nil {

		t.Fatalf("verify open: %v", err)
	}
	defer l.Close()

	got, ok := l.Get("race-test")
	if !ok {

		t.Fatalf("race-test section missing after concurrent writes")
	}

	if string(got) != `"ok"` {

		t.Errorf("race-test: want %q, got %q", `"ok"`, got)
	}
}

// TestValidateData_SectionCap verifies oversized payloads are rejected.
func TestValidateData_SectionCap(t *testing.T) {

	// Payload just under the cap should pass.
	small := make([]byte, MaxSectionBytes-1)
	for i := range small {
		small[i] = 'a'
	}
	if err := ValidateData(json.RawMessage(small), 0); err != nil {
		t.Errorf("small payload: want nil, got %v", err)
	}

	// Payload at the cap should pass.
	atCap := make([]byte, MaxSectionBytes)
	for i := range atCap {
		atCap[i] = 'b'
	}
	if err := ValidateData(json.RawMessage(atCap), 0); err != nil {
		t.Errorf("at-cap payload: want nil, got %v", err)
	}

	// Payload over the cap should fail.
	over := make([]byte, MaxSectionBytes+1)
	for i := range over {
		over[i] = 'c'
	}
	if err := ValidateData(json.RawMessage(over), 0); err == nil {
		t.Errorf("oversized payload: want error, got nil")
	}
}

// TestValidateData_SectionCountCap verifies max section count is enforced.
func TestValidateData_SectionCountCap(t *testing.T) {

	small := json.RawMessage(`"ok"`)

	// Under the cap should pass.
	if err := ValidateData(small, MaxSections-1); err != nil {
		t.Errorf("under cap: want nil, got %v", err)
	}

	// At the cap should fail (cannot add more).
	if err := ValidateData(small, MaxSections); err == nil {
		t.Errorf("at cap: want error, got nil")
	}

	// Over the cap should fail.
	if err := ValidateData(small, MaxSections+1); err == nil {
		t.Errorf("over cap: want error, got nil")
	}

	// Negative existing count (skip check) should pass.
	if err := ValidateData(small, -1); err != nil {
		t.Errorf("skip check: want nil, got %v", err)
	}
}

// TestFormatContext_Empty verifies empty ledger returns empty string.
func TestFormatContext_Empty(t *testing.T) {

	dir := t.TempDir()
	l, err := OpenAt(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	got := FormatContext(l, 0)
	if got != "" {
		t.Errorf("empty ledger: want empty, got %q", got)
	}
}

// TestFormatContext_WithSections verifies context formatting.
func TestFormatContext_WithSections(t *testing.T) {

	dir := t.TempDir()
	l, err := OpenAt(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	l.Set("hosts", json.RawMessage(`{"web1":"nginx"}`))
	l.Set("roles", json.RawMessage(`["db","cache"]`))

	got := FormatContext(l, 0)
	if got == "" {
		t.Fatal("want non-empty context, got empty")
	}
	if !strings.Contains(got, "Known environment (ledger):") {
		t.Errorf("missing header in %q", got)
	}
	if !strings.Contains(got, "hosts:") {
		t.Errorf("missing hosts in %q", got)
	}
	if !strings.Contains(got, "roles:") {
		t.Errorf("missing roles in %q", got)
	}
}

// TestFormatContext_Budget verifies truncation at budget limit.
func TestFormatContext_Budget(t *testing.T) {

	dir := t.TempDir()
	l, err := OpenAt(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	// Add a large section.
	large := make([]byte, 1000)
	for i := range large {
		large[i] = 'x'
	}
	l.Set("big", json.RawMessage(`"`+string(large)+`"`))

	// With a tiny budget, should truncate.
	got := FormatContext(l, 50)
	if !strings.Contains(got, "truncated") {
		t.Errorf("want truncation marker, got %q", got)
	}
}

// TestFormatContext_BudgetNoExceed verifies the block never exceeds
// maxBytes: when the truncation marker itself does not fit, it is
// omitted rather than overflowing the budget.
func TestFormatContext_BudgetNoExceed(t *testing.T) {

	dir := t.TempDir()
	l, err := OpenAt(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	large := make([]byte, 1000)
	for i := range large {
		large[i] = 'x'
	}
	l.Set("big", json.RawMessage(`"`+string(large)+`"`))

	// Header (27 bytes) + marker (14 bytes) > 40, so the marker
	// must be dropped to stay within budget.
	got := FormatContext(l, 40)
	if len(got) > 40 {
		t.Errorf("block is %d bytes, exceeds budget 40: %q", len(got), got)
	}

	// Budget large enough for the marker: emitted and counted.
	got2 := FormatContext(l, 50)
	if !strings.Contains(got2, "truncated") {
		t.Errorf("want truncation marker, got %q", got2)
	}
	if len(got2) > 50 {
		t.Errorf("block is %d bytes, exceeds budget 50: %q", len(got2), got2)
	}
}

// TestFormatContext_Precision verifies integers > 2^53 keep full
// precision in the injected context (no float64 rounding).
func TestFormatContext_Precision(t *testing.T) {

	dir := t.TempDir()
	l, err := OpenAt(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	l.Set("bigint", json.RawMessage(`{"id":9007199254740993}`))

	got := FormatContext(l, 0)
	if !strings.Contains(got, "9007199254740993") {
		t.Errorf("integer precision lost in context: %q", got)
	}
}

// TestReadData_OversizedFile_Rejected verifies a tampered encrypted
// file beyond the size cap fails fast instead of being read into
// memory.
func TestReadData_OversizedFile_Rejected(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, dataFile)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.Truncate(maxEncryptedBytes + 1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	f.Close()

	if _, err := readData(dir, id); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Errorf("want oversized-file error, got %v", err)
	}
}

// TestReadData_OversizedPlaintext_Rejected verifies an encrypted
// file that decrypts beyond the plaintext cap is rejected instead of
// loaded whole into memory.
func TestReadData_OversizedPlaintext_Rejected(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, dataFile)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w, err := age.Encrypt(f, id.Recipient())
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	zeros := io.LimitReader(zeroReader{}, maxDecryptedBytes+1)
	if _, err := io.Copy(w, zeros); err != nil {
		t.Fatalf("write plaintext: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close age writer: %v", err)
	}
	f.Close()

	if _, err := readData(dir, id); err == nil ||
		!strings.Contains(err.Error(), "decrypted ledger exceeds") {
		t.Errorf("want oversized-plaintext error, got %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}

	return len(p), nil
}
