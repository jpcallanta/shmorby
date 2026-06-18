package tools

import (
	"bytes"
	"strings"
	"testing"
)

func setMaxOutput(t *testing.T, n int) {
	t.Helper()
	old := MaxOutput
	MaxOutput = n
	t.Cleanup(func() { MaxOutput = old })
}

func TestTruncateOutput_Unlimited(t *testing.T) {
	setMaxOutput(t, 0)
	in := make([]byte, 100000)
	for i := range in {
		in[i] = 'x'
	}
	got := TruncateOutput(in)
	if !bytes.Equal(got, in) {
		t.Errorf("expected no truncation when MaxOutput=0")
	}
}

func TestTruncateOutput_UnderLimit(t *testing.T) {
	setMaxOutput(t, 65536)
	in := []byte("hello world")
	got := TruncateOutput(in)
	if !bytes.Equal(got, in) {
		t.Errorf("want %q, got %q", in, got)
	}
}

func TestTruncateOutput_AtLimit(t *testing.T) {
	setMaxOutput(t, 65536)
	in := make([]byte, MaxOutput)
	for i := range in {
		in[i] = 'a'
	}
	got := TruncateOutput(in)
	if !bytes.Equal(got, in) {
		t.Errorf("expected no truncation at exactly MaxOutput bytes")
	}
}

func TestTruncateOutput_OverLimit(t *testing.T) {
	setMaxOutput(t, 65536)
	in := make([]byte, MaxOutput+10000)
	for i := range in {
		in[i] = 'a'
	}
	got := TruncateOutput(in)
	if len(got) >= len(in) {
		t.Errorf("expected truncated output, got len %d >= %d", len(got), len(in))
	}
	if !strings.HasSuffix(string(got), truncNotice) {
		t.Errorf("expected truncation notice suffix, got %q", string(got[len(got)-50:]))
	}
	if len(got) != MaxOutput {
		t.Errorf("expected output len %d, got %d", MaxOutput, len(got))
	}
}

func TestTruncateOutput_EmptyInput(t *testing.T) {
	setMaxOutput(t, 65536)
	in := []byte{}
	got := TruncateOutput(in)
	if !bytes.Equal(got, in) {
		t.Errorf("want empty, got %q", got)
	}
}

func TestTruncateOutput_TruncationNoticeLength(t *testing.T) {
	setMaxOutput(t, 65536)
	// Verify the notice fits within the limit (no panic on negative limit).
	in := make([]byte, MaxOutput+1)
	for i := range in {
		in[i] = 'b'
	}
	got := TruncateOutput(in)
	if len(got) != MaxOutput {
		t.Errorf("want %d bytes, got %d", MaxOutput, len(got))
	}
	if !strings.HasSuffix(string(got), truncNotice) {
		t.Errorf("missing truncation notice")
	}
}
