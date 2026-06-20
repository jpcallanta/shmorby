//go:build !windows && !unix

package xdg

import (
	"os"
	"path/filepath"
)

// DefaultWorkDir returns the default working directory for shell tool
// commands.
func DefaultWorkDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".local/share/shmorby/workdir"
	}
	return filepath.Join(home, ".local", "share", "shmorby", "workdir")
}

// SystemConfigDir returns the system-level config directory.
func SystemConfigDir() string {
	return "/etc/shmorby"
}

// UserConfigDir returns the user-level config directory.
func UserConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config/shmorby"
	}
	return filepath.Join(home, ".config", "shmorby")
}

// UserDataDir returns the user-local data directory.
func UserDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".local/share/shmorby"
	}
	return filepath.Join(home, ".local", "share", "shmorby")
}

// RootPrefix returns the filesystem root prefix for scope walking.
func RootPrefix() string {
	return "/"
}

// DefaultShell returns the OS-preferred shell command.
func DefaultShell() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "bash"
	}
	return sh
}
