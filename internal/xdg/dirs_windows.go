//go:build windows

package xdg

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Returns the default working directory for shell tool
// commands.
func DefaultWorkDir() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home, _ := os.UserHomeDir()
		localAppData = filepath.Join(home, "AppData", "Local")
	}

	return filepath.Join(localAppData, "shmorby", "workdir")
}

// Returns the system-level config directory.
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

// Returns the user-level config directory.
func UserConfigDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "shmorby")
}

// Returns the user-local data directory.
func UserDataDir() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home, _ := os.UserHomeDir()
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(localAppData, "shmorby")
}

// Returns the filesystem root prefix for scope walking.
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

// Returns the OS-preferred shell command, preferring
// pwsh over powershell when available.
func DefaultShell() string {
	if _, err := exec.LookPath("pwsh.exe"); err == nil {
		return "pwsh.exe"
	}

	if _, err := exec.LookPath("powershell.exe"); err == nil {
		return "powershell.exe"
	}

	comspec := os.Getenv("ComSpec")
	if comspec != "" {
		return comspec
	}

	return "powershell.exe"
}
