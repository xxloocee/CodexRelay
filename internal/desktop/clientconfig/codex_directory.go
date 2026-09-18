package clientconfig

import (
	"os"
	"path/filepath"
	"strings"

	"codexrelay/internal/config"
)

// Persisted paths stay pinned while backups refer to them. Older saved paths
// have unknown provenance and are treated as explicit, never silently moved.
func ResolveCodexDirectory(entry config.ClientConfig) (string, string) {
	if directory := strings.TrimSpace(entry.ConfigDir); directory != "" {
		source := entry.ConfigDirSource
		if source != "environment" && source != "default" {
			source = "explicit"
		}
		return filepath.Clean(directory), source
	}
	if directory := strings.TrimSpace(os.Getenv("CODEX_HOME")); directory != "" && filepath.IsAbs(directory) {
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			return filepath.Clean(directory), "environment"
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex"), "default"
}

func defaultCodexDirectory() string {
	directory, _ := ResolveCodexDirectory(config.ClientConfig{})
	return directory
}
