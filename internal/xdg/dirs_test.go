//go:build unix

package xdg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultWorkDir_Unix(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	got := DefaultWorkDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "shmorby", "workdir")
	if got != want {
		t.Errorf("DefaultWorkDir: got %q, want %q", got, want)
	}
}

func TestDefaultWorkDir_XDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got := DefaultWorkDir()
	want := "/custom/data/shmorby/workdir"
	if got != want {
		t.Errorf("DefaultWorkDir with XDG_DATA_HOME: got %q, want %q", got, want)
	}
}

func TestSystemConfigDir_Unix(t *testing.T) {
	got := SystemConfigDir()
	want := "/etc/shmorby"
	if got != want {
		t.Errorf("SystemConfigDir: got %q, want %q", got, want)
	}
}

func TestUserConfigDir_Unix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := UserConfigDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".config", "shmorby")
	if got != want {
		t.Errorf("UserConfigDir: got %q, want %q", got, want)
	}
}

func TestUserConfigDir_XDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got := UserConfigDir()
	want := "/custom/config/shmorby"
	if got != want {
		t.Errorf("UserConfigDir with XDG_CONFIG_HOME: got %q, want %q", got, want)
	}
}

func TestUserDataDir_Unix(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	got := UserDataDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "shmorby")
	if got != want {
		t.Errorf("UserDataDir: got %q, want %q", got, want)
	}
}

func TestUserDataDir_XDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got := UserDataDir()
	want := "/custom/data/shmorby"
	if got != want {
		t.Errorf("UserDataDir with XDG_DATA_HOME: got %q, want %q", got, want)
	}
}

func TestRootPrefix_Unix(t *testing.T) {
	got := RootPrefix()
	want := "/"
	if got != want {
		t.Errorf("RootPrefix: got %q, want %q", got, want)
	}
}

func TestDefaultShell_Unix(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	got := DefaultShell()
	want := "/bin/zsh"
	if got != want {
		t.Errorf("DefaultShell with SHELL: got %q, want %q", got, want)
	}
}

func TestDefaultShell_UnixFallback(t *testing.T) {
	t.Setenv("SHELL", "")
	got := DefaultShell()
	want := "bash"
	if got != want {
		t.Errorf("DefaultShell fallback: got %q, want %q", got, want)
	}
}

func TestConfigHome_XDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	got := configHome()
	want := "/xdg/config"
	if got != want {
		t.Errorf("configHome with XDG_CONFIG_HOME: got %q, want %q", got, want)
	}
}

func TestConfigHome_Fallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := configHome()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".config")
	if got != want {
		t.Errorf("configHome fallback: got %q, want %q", got, want)
	}
}

func TestUserDataHome_XDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	got := userDataHome()
	want := "/xdg/data"
	if got != want {
		t.Errorf("userDataHome with XDG_DATA_HOME: got %q, want %q", got, want)
	}
}

func TestUserDataHome_Fallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	got := userDataHome()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share")
	if got != want {
		t.Errorf("userDataHome fallback: got %q, want %q", got, want)
	}
}

func TestAllFunctions_ReturnNonEmpty(t *testing.T) {
	if got := DefaultWorkDir(); got == "" {
		t.Error("DefaultWorkDir: want non-empty")
	}
	if got := SystemConfigDir(); got == "" {
		t.Error("SystemConfigDir: want non-empty")
	}
	if got := UserConfigDir(); got == "" {
		t.Error("UserConfigDir: want non-empty")
	}
	if got := UserDataDir(); got == "" {
		t.Error("UserDataDir: want non-empty")
	}
	if got := RootPrefix(); got == "" {
		t.Error("RootPrefix: want non-empty")
	}
	if got := DefaultShell(); got == "" {
		t.Error("DefaultShell: want non-empty")
	}
}

func TestPaths_ContainShmorby(t *testing.T) {
	if !strings.Contains(DefaultWorkDir(), "shmorby") {
		t.Errorf("DefaultWorkDir %q missing shmorby", DefaultWorkDir())
	}
	if !strings.Contains(SystemConfigDir(), "shmorby") {
		t.Errorf("SystemConfigDir %q missing shmorby", SystemConfigDir())
	}
	if !strings.Contains(UserConfigDir(), "shmorby") {
		t.Errorf("UserConfigDir %q missing shmorby", UserConfigDir())
	}
	if !strings.Contains(UserDataDir(), "shmorby") {
		t.Errorf("UserDataDir %q missing shmorby", UserDataDir())
	}
}
