package tools

import (
	"fmt"
	"regexp"
)

// Mutating patterns to block in diagnose mode.
// Matches at start, or after ; && || | ( to catch pipelines and
// subshells.
var (
	mutatingCmd = regexp.MustCompile(`(?:^|;|&&|\|\||\||\()\s*\b(rm|mv|dd|mkfs)\b`)
	pkgInstall  = regexp.MustCompile(`(?:^|;|&&|\|\||\||\()\s*\b(apt-get|apt|yum|dnf|pacman|zypper|emerge|apk)\s+(install|remove|purge)\b`)
	svcMutate   = regexp.MustCompile(`(?:^|;|&&|\|\||\||\()\s*\bsystemctl\s+(start|stop|restart|enable|disable|mask|reload)\b`)
	etcRedirect = regexp.MustCompile(`(?:^|;|&&|\|\||\||\()\s*[^#]*>\s*/etc/`)
)

// Known v1 heuristic gaps (deferred to phase 2 bash analysis per
// SPEC §10): tee /etc, npm/pip/brew install, chmod, curl -o /etc,
// wget, kubectl delete, etc.
//
// Returns an error if cmd contains a mutating pattern blocked in
// diagnose mode.
func CheckMutating(cmd string) error {
	if mutatingCmd.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: blocklisted command verb (rm/mv/dd/mkfs) in: %s",
			cmd,
		)
	}
	if pkgInstall.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: package install/remove verb blocked in: %s",
			cmd,
		)
	}
	if svcMutate.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: systemctl mutating verb blocked in: %s",
			cmd,
		)
	}
	if etcRedirect.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: shell redirect to /etc blocked in: %s",
			cmd,
		)
	}

	return nil
}
