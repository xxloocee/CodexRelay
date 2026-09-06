package desktop

import (
	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
	"path/filepath"
)

// rememberOfficialConfig keeps the first pre-relay backup for each file while
// refreshing the fingerprint of the latest CodexRelay write. This lets an
// explicit switch back to the official client configuration detect external
// edits without replacing a newer user change silently. The first write also
// fixes the file set: later adapter versions or client-created files are not
// silently promoted into the official snapshot.
func rememberOfficialConfig(cfg *config.AppConfig, category string, result clientconfig.ConfigureResult) {
	if cfg == nil {
		return
	}
	if cfg.ClientConfigs == nil {
		cfg.ClientConfigs = map[string]config.ClientConfig{}
	}
	entry := cfg.ClientConfigs[category]
	if !result.OfficialSnapshot {
		// A transaction without an official snapshot remains managed but cannot
		// be offered as a restore point.
		entry.Mode = "relay"
		cfg.ClientConfigs[category] = entry
		return
	}
	if len(result.Files) == 0 {
		return
	}
	if entry.Mode == "official" || result.ResetOfficialSnapshot {
		// A third-party switcher may have changed the official files after the
		// previous Relay takeover. The successful re-takeover above captured a
		// fresh baseline, so discard metadata for the older generation.
		entry.OfficialBackups = nil
	}
	byPath := make(map[string]int, len(entry.OfficialBackups))
	for index, backup := range entry.OfficialBackups {
		byPath[backup.Path] = index
	}
	for _, file := range result.Files {
		backup := config.ClientConfigBackup{
			Path: file.Path, BackupPath: file.BackupPath, Existed: file.Existed,
			Mode: file.Mode, ExpectedSHA256: file.ExpectedSHA256, BackupSHA256: file.BackupSHA256,
		}
		if file.BackupPath != "" {
			backup.BackupPath = filepath.Join("client-backups", category, filepath.Base(file.BackupPath))
		}
		if index, ok := byPath[file.Path]; ok {
			// A file absent during first takeover is Relay-created. Keep that
			// fact permanently so switching back to the official client removes
			// the generated file instead of restoring Relay contents as "official".
			if !entry.OfficialBackups[index].Existed && file.Existed {
				entry.OfficialBackups[index].ExpectedSHA256 = file.ExpectedSHA256
				continue
			}
			entry.OfficialBackups[index].ExpectedSHA256 = file.ExpectedSHA256
			continue
		}
		// Capture files first touched later (for example Gemini settings.json)
		// before Relay modifies them, preserving their user-created content.
		entry.OfficialBackups = append(entry.OfficialBackups, backup)
		byPath[file.Path] = len(entry.OfficialBackups) - 1
	}
	entry.Mode = "relay"
	cfg.ClientConfigs[category] = entry
}
