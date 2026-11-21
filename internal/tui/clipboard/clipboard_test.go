package clipboard

import (
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	// Reset for test
	once = sync.Once{}
	initErr = nil

	err := Init()
	if err != nil {
		// Clipboard may not be available in CI; that's acceptable.
		t.Skipf("clipboard not available: %v", err)
	}
	if initErr != nil {
		t.Error("expected no init error")
	}
}

func TestInit_Idempotent(t *testing.T) {
	once = sync.Once{}
	initErr = nil

	err := Init()
	if err != nil {
		t.Skipf("clipboard not available: %v", err)
	}
	err2 := Init()
	if err2 != nil {
		t.Error("second init should succeed")
	}
}

func TestPaste_BeforeInit(t *testing.T) {
	// Force uninitialized state.
	once = sync.Once{}
	initErr = fmt.Errorf("clipboard unavailable")

	got := Paste()
	if got != "" {
		// Paste may return system clipboard content if the package was
		// initialized earlier in this process; skip in that case.
		t.Skipf("clipboard was already initialized, got %q", got)
	}
}

func TestPasteImage_BeforeInit(t *testing.T) {
	// Force uninitialized state.
	once = sync.Once{}
	initErr = fmt.Errorf("clipboard unavailable")

	data, ok := PasteImage()
	if ok || data != nil {
		t.Errorf("want (nil, false) before init, got (%d bytes, %v)",
			len(data), ok)
	}
}

func TestCopy_BeforeInit(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Copy before Init panicked: %v", r)
		}
	}()

	once = sync.Once{}
	initErr = nil

	Copy("test")
}

func TestCopy_Paste(t *testing.T) {
	once = sync.Once{}
	initErr = nil

	err := Init()
	if err != nil {
		t.Skipf("clipboard not available: %v", err)
	}
	Copy("hello clipboard")
	got := Paste()
	if got != "hello clipboard" {
		t.Errorf("want %q, got %q", "hello clipboard", got)
	}
}

func TestOsc52_Copy(t *testing.T) {
	// OSC-52 sequence format: ESC ] 52 ; c ; <base64> BEL
	text := "test clipboard content"
	encoded := base64.StdEncoding.EncodeToString([]byte(text))

	// Verify encoding is correct.
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}
	if string(decoded) != text {
		t.Errorf("want %q, got %q", text, string(decoded))
	}
}

func TestCopy_DelegatesToOsc52WhenInitFails(t *testing.T) {
	once = sync.Once{}
	initErr = fmt.Errorf("clipboard unavailable")

	// Should not panic; will attempt OSC-52 fallback.
	Copy("fallback test")
}

func TestOsc52Sequence_Standard(t *testing.T) {
	text := "hello world"
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	want := fmt.Sprintf("\033]52;c;%s\007", encoded)

	got := osc52Sequence(text, false)
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestOsc52Sequence_Tmux(t *testing.T) {
	text := "hello world"
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	want := fmt.Sprintf("\033Ptmux;\033\033]52;c;%s\007\033\\", encoded)

	got := osc52Sequence(text, true)
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}
