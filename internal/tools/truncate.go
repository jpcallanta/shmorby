package tools

import "sync/atomic"

// MaxOutput is the maximum output size in bytes. 0 = unlimited.
// Atomic because TruncateOutput reads it from subagent goroutines
// while main stores the config value at startup; a plain int is
// race-prone if initialization ordering ever changes.
var MaxOutput atomic.Int64

const truncNotice = "\n... (output truncated at 64 KiB)"

// Caps output at MaxOutput bytes. If the input exceeds the limit, it
// appends a truncation notice. When MaxOutput is 0, the output is
// returned unchanged. Loads the limit once so a concurrent update
// cannot make the check and the slice disagree.
func TruncateOutput(out []byte) []byte {
	cap64 := MaxOutput.Load()
	if cap64 <= 0 || int64(len(out)) <= cap64 {
		return out
	}

	limit := int(cap64) - len(truncNotice)
	if limit < 0 {
		limit = 0
	}
	result := make([]byte, limit+len(truncNotice))
	copy(result, out[:limit])
	copy(result[limit:], truncNotice)

	return result
}
