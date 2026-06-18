package memory

import (
	"regexp"
	"strings"
	"sync"
)

// TagRule maps a regex pattern to a named capture tag.
type TagRule struct {
	Pattern string `yaml:"pattern"`
	Tag     string `yaml:"tag"`
}

// EmbeddingConfig holds embedding provider settings.
type EmbeddingConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url"`
}

// Config holds memory store configuration.
type Config struct {
	Enabled     bool            `yaml:"enabled"`
	DBPath      string          `yaml:"db_path"`
	MaxEntries  int             `yaml:"max_entries"`
	AutoCapture bool            `yaml:"auto_capture"`
	Tags        []TagRule       `yaml:"tags"`
	Embedding   EmbeddingConfig `yaml:"embedding"`
}

// defaultConfig returns a Config with standard defaults.
func defaultConfig() Config {
	return Config{
		Enabled:     true,
		DBPath:      "~/.local/share/shmorby/memory.db",
		MaxEntries:  10000,
		AutoCapture: true,
	}
}

// MaxResultLen is the cap for stored result text.
const MaxResultLen = 4096

// Caps a string at MaxResultLen bytes. No marker appended — matches
// spec contract: truncate(result, 4096).
func truncateResult(s string) string {
	if len(s) <= MaxResultLen {
		return s
	}

	return s[:MaxResultLen]
}

var tagCache sync.Map

// Extracts tags from a command string by matching configured patterns.
func extractTags(command string, rules []TagRule) []string {
	if len(rules) == 0 {
		return nil
	}

	var tags []string

	for _, rule := range rules {
		reI, ok := tagCache.Load(rule.Pattern)
		var re *regexp.Regexp
		if !ok {
			var err error
			re, err = regexp.Compile(rule.Pattern)
			if err != nil {
				continue
			}
			tagCache.Store(rule.Pattern, re)
		} else {
			re = reI.(*regexp.Regexp)
		}

		matches := re.FindStringSubmatch(command)
		if matches == nil {
			continue
		}

		tag := rule.Tag
		for i, m := range matches[1:] {
			tag = strings.ReplaceAll(tag, "$"+itoa(i+1), m)
		}

		tags = append(tags, tag)
	}

	return tags
}

// Simple int-to-string for tag replacement (avoids fmt import).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var buf [12]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	return string(buf[i:])
}
