package memory

func defaultConfig() Config {
	return Config{
		Enabled:     true,
		DBPath:      "~/.local/share/shmorby/memory.db",
		MaxEntries:  10000,
		AutoCapture: true,
	}
}
