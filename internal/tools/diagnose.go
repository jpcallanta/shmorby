package tools

import (
	"fmt"
	"regexp"
)

// SECURITY: The separator anchor in patterns below includes \r?\n
// because newlines are valid shell statement terminators (equivalent
// to ;). Without this, a malicious command can bypass the blocklist
// by prepending a newline before the dangerous command.
//
// Matches at start, or after ; && || | ( \n \r\n to catch pipelines,
// subshells, and newline-separated commands.
var (
	mutatingCmd = regexp.MustCompile(`(?:^|;|&&|\|\||\||\(|\r?\n)\s*\b(rm|mv|dd|mkfs)\b`)
	pkgInstall  = regexp.MustCompile(`(?:^|;|&&|\|\||\||\(|\r?\n)\s*\b(apt-get|apt|yum|dnf|pacman|zypper|emerge|apk)\s+(install|remove|purge)\b`)
	svcMutate   = regexp.MustCompile(`(?:^|;|&&|\|\||\||\(|\r?\n)\s*\bsystemctl\s+(start|stop|restart|enable|disable|mask|reload)\b`)
	etcRedirect = regexp.MustCompile(`(?:^|;|&&|\|\||\||\(|\r?\n)\s*[^#]*>\s*/etc/`)

	// Additional patterns to close known bypass vectors.
	// These catch command substitution, eval, xargs,
	// chmod/chown, and curl/wget piping to shell.
	evalCmd = regexp.MustCompile(
		`(?:^|;|&&|\|\||\||\(|\r?\n)\s*\beval\b`,
	)
	// $() command substitution containing mutating verbs
	cmdSubst = regexp.MustCompile(
		`\$\(\s*[^)]*\b(rm|mv|dd|mkfs|chmod|chown|tee|curl|wget)\b`,
	)
	// backtick command substitution containing mutating verbs
	backtickSubst = regexp.MustCompile(
		"`[^`" + `]*` + `\b(rm|mv|dd|mkfs|chmod|chown|tee|curl|wget)\b`,
	)
	// Any command piped to xargs followed by a mutating verb.
	// This closes the gap where ls|find|grep|etc piped to xargs
	// rm/mv/etc was not caught by the old pattern that only
	// matched echo|printf|cat before the pipe.
	xargsMutate = regexp.MustCompile(
		`\|\s*\bxargs\b.*\b(rm|mv|dd|mkfs|chmod|chown)\b`,
	)
	// chmod / chown on sensitive paths
	permMutate = regexp.MustCompile(
		`(?:^|;|&&|\|\||\||\(|\r?\n)\s*\b(chmod|chown)\b` +
			`.*(?:/(etc|var|usr|boot|root|home)\b)`,
	)
	// tee writing to sensitive paths
	teeSensitive = regexp.MustCompile(
		`(?:^|;|&&|\|\||\||\(|\r?\n)\s*.*\|\s*\btee\b` +
			`.*/(?:etc|var|usr|boot|root|home)/`,
	)
	// curl/wget piped to shell — remote code execution vector
	curlWgetShell = regexp.MustCompile(
		`(?:^|;|&&|\|\||\||\(|\r?\n)\s*\b(curl|wget)\b` +
			`.*\|\s*\b(bash|sh|zsh|dash)\b`,
	)
)

// Known v1 heuristic gaps: indirect python/ruby/perl one-liners (python -c,
// ruby -e, perl -e), kubectl delete, docker rm, find -exec,
// find -delete, sed -i on sensitive paths, kill/killall,
// useradd/userdel, $(kill ...), $(sed -i ...), $(useradd ...),
// and other container orchestrator mutations.
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
	// Close bypass vectors — eval, command substitution, xargs,
	// chmod/chown, tee, curl|sh.
	if evalCmd.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: eval blocked in: %s", cmd,
		)
	}
	if cmdSubst.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: $() command substitution with mutating verb blocked in: %s",
			cmd,
		)
	}
	if backtickSubst.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: backtick command substitution with mutating verb blocked in: %s",
			cmd,
		)
	}
	if xargsMutate.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: pipe to xargs with mutating verb blocked in: %s",
			cmd,
		)
	}
	if permMutate.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: chmod/chown on sensitive path blocked in: %s",
			cmd,
		)
	}
	if teeSensitive.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: tee to sensitive path blocked in: %s",
			cmd,
		)
	}
	if curlWgetShell.MatchString(cmd) {
		return fmt.Errorf(
			"diagnose: curl/wget piped to shell blocked in: %s",
			cmd,
		)
	}

	return nil
}
