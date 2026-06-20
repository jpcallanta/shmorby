//go:build unix

package xdg

import (
	"os"
	"path/filepath"
)

// DefaultWorkDir returns the default working directory for shell tool
// commands.
func DefaultWorkDir() string {
	return filepath.Join(userDataHome(), "shmorby", "workdir")
}

// SystemConfigDir returns the system-level config directory.
func SystemConfigDir() string {
	return "/etc/shmorby"
}

// UserConfigDir returns the user-level config directory.
func UserConfigDir() string {
	return filepath.Join(configHome(), "shmorby")
}

// UserDataDir returns the user-local data directory.
func UserDataDir() string {
	return filepath.Join(userDataHome(), "shmorby")
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

func configHome() string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg != "" {
		return xdg
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config"
	}
	return filepath.Join(home, ".config")
}

func userDataHome() string {
	xdg := os.Getenv("XDG_DATA_HOME")
	if xdg != "" {
		return xdg
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".local/share"
	}
	return filepath.Join(home, ".local", "share")
}
