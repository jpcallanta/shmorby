package tools

// MaxOutput is the maximum output size in bytes. 0 = unlimited.
var MaxOutput int

const truncNotice = "\n... (output truncated at 64 KiB)"

// Caps output at MaxOutput bytes. If the input exceeds the limit, it
// appends a truncation notice. When MaxOutput is 0, the output is
// returned unchanged.
func TruncateOutput(out []byte) []byte {
	if MaxOutput <= 0 || len(out) <= MaxOutput {
		return out
	}

	limit := MaxOutput - len(truncNotice)
	if limit < 0 {
		limit = 0
	}
	result := make([]byte, limit+len(truncNotice))
	copy(result, out[:limit])
	copy(result[limit:], truncNotice)

	return result
}
