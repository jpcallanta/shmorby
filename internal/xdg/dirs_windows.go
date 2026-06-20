//go:build windows

package xdg

import (
	"os"
	"path/filepath"
)

// DefaultWorkDir returns the default working directory for shell tool
// commands.
func DefaultWorkDir() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "shmorby", "workdir")
}

// SystemConfigDir returns the system-level config directory.
func SystemConfigDir() string {
	progData := os.Getenv("ProgramData")
	if progData == "" {
		progData = filepath.Join(os.Getenv("SystemDrive")+`\`, "ProgramData")
	}
	if progData == `\ProgramData` {
		if home, err := os.UserHomeDir(); err == nil {
			progData = filepath.Join(home, "AppData", "Local", "ProgramData")
		}
	}
	return filepath.Join(progData, "shmorby")
}

// UserConfigDir returns the user-level config directory.
func UserConfigDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "shmorby")
}

// UserDataDir returns the user-local data directory.
func UserDataDir() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home, _ := os.UserHomeDir()
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(localAppData, "shmorby")
}

// RootPrefix returns the filesystem root prefix for scope walking.
func RootPrefix() string {
	wd, err := os.Getwd()
	if err != nil {
		return `\`
	}
	vol := filepath.VolumeName(wd)
	if vol == "" {
		return `\`
	}
	return vol + `\`
}

// DefaultShell returns the OS-preferred shell command.
func DefaultShell() string {
	comspec := os.Getenv("ComSpec")
	if comspec != "" {
		return comspec
	}
	return "powershell.exe"
}
