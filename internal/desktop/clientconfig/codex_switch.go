package clientconfig

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"codexrelay/internal/config"
)

// Official selection always uses native OpenAI. Backups supply optional OAuth
// credentials only; an external provider snapshot is never an official target.
func SwitchCodexOfficialWithRollback(cfg config.AppConfig, dataDirectory string) (func() error, error) {
	var savedAuth []byte
	savedStore := ""
	if files, err := ResolveOfficialConfigFiles(cfg, config.CategoryCodex, dataDirectory); err == nil {
		for _, backup := range files {
			if !backup.Existed {
				continue
			}
			data, readErr := os.ReadFile(backup.BackupPath)
			if readErr != nil {
				continue
			}
			if filepath.Base(backup.Path) == "auth.json" {
				savedAuth = data
			} else if value, parseErr := readCodexTOML(data); parseErr == nil && codexSelectedProvider(value) == "openai" {
				savedStore = stringField(value, "cli_auth_credentials_store")
				if profile := codexActiveProfile(value); profile != nil {
					if override := stringField(profile, "cli_auth_credentials_store"); override != "" {
						savedStore = override
					}
				}
			}
		}
	}
	return configureCodexNativeWithAuth(cfg, ClientBackupDirectory(dataDirectory, config.CategoryCodex), savedAuth, savedStore)
}

// Provider names are labels, never paths. Keep old flat backup paths readable.
func codexBackupPrefix(data []byte) string {
	provider := "unknown"
	if value, err := readCodexTOML(data); err == nil {
		provider = codexSelectedProvider(value)
	}
	label := ""
	for _, char := range provider {
		part := url.QueryEscape(string(char))
		if len(label)+len(part) > 96 {
			break
		}
		label += part
	}
	return "provider-" + label + "--"
}

// Switching depends on the live files, not the availability of an old backup.
// Historical sessions are repaired separately, never as a switch prerequisite.
func configureCodexLive(file, endpoint, key, model, backupRoot string) (ConfigureResult, error) {
	authPath := filepath.Join(filepath.Dir(file), "auth.json")
	backupArgs := []string{backupRoot, ""}
	newBaseline := false
	result, err := applyCodexConfigTransaction([]string{file, authPath}, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		source, auth := snapshots[file].data, snapshots[authPath].data
		if matches, _ := codexRelayConfigurationMatches(source, auth, endpoint, key, model, model == ""); matches {
			return nil, nil
		}
		backupArgs[1] = codexBackupPrefix(source)
		value, parseErr := readCodexTOML(source)
		if parseErr == nil {
			provider := codexSelectedProvider(value)
			newBaseline = snapshots[file].existed && provider != codexRelayModelProviderID && provider != codexLegacyModelProviderID
		} else {
			// Preserve the unreadable original on disk, then build a valid target.
			source = nil
		}
		data, nextAuth, err := renderCodexData(file, source, auth, endpoint, key, strings.TrimSpace(model))
		if err == nil {
			if rendered, parseErr := readCodexTOML(data); parseErr == nil && reflect.DeepEqual(value, rendered) {
				data = snapshots[file].data
			}
			var oldAuth, desiredAuth map[string]any
			if json.Unmarshal(auth, &oldAuth) == nil && json.Unmarshal(nextAuth, &desiredAuth) == nil && reflect.DeepEqual(oldAuth, desiredAuth) {
				nextAuth = auth
			}
		}
		return []ConfigFileChange{{Path: file, Data: data}, {Path: authPath, Data: nextAuth}}, err
	}, backupArgs...)
	if err != nil {
		return result, err
	}
	// Every external provider gets a fresh paired snapshot. Relay recovery
	// copies remain on disk without replacing a previous external login.
	result.OfficialSnapshot = newBaseline
	result.ResetOfficialSnapshot = newBaseline
	return result, nil
}

// Missing original credentials cannot be recreated. Select native OpenAI and
// let Codex request login, retaining any live OAuth login and all user settings.
func ConfigureCodexNativeWithRollback(cfg config.AppConfig, backupRoot string) (func() error, error) {
	return configureCodexNativeWithAuth(cfg, backupRoot, nil, "")
}

func configureCodexNativeWithAuth(cfg config.AppConfig, backupRoot string, savedAuth []byte, savedStore string) (func() error, error) {
	definition, _ := clientDefinitionFor(config.CategoryCodex)
	directory, file := clientConfigPath(definition, cfg.ClientConfigs[config.CategoryCodex])
	authPath := filepath.Join(directory, "auth.json")
	backupArgs := []string{backupRoot, ""}
	result, err := applyCodexConfigTransaction([]string{file, authPath}, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		backupArgs[1] = codexBackupPrefix(snapshots[file].data)
		value, parseErr := readCodexTOML(snapshots[file].data)
		if parseErr != nil {
			value = map[string]any{}
		}
		original, _ := readCodexTOML(snapshots[file].data)
		store := stringField(value, "cli_auth_credentials_store")
		if profile := codexActiveProfile(value); profile != nil {
			if override := stringField(profile, "cli_auth_credentials_store"); override != "" {
				store = override
			}
		}
		if (!snapshots[file].existed || parseErr != nil || codexSelectedProvider(value) != "openai") && (savedStore == "keyring" || savedStore == "auto") {
			store = savedStore
		}
		clearCodexActiveConnection(value)
		// Remove only overrides of the built-in OpenAI provider.
		if providers, ok := value["model_providers"].(map[string]any); ok {
			delete(providers, "openai")
		}
		if profile := codexActiveProfile(value); profile != nil {
			if providers, ok := profile["model_providers"].(map[string]any); ok {
				delete(providers, "openai")
			}
		}
		value["model_provider"] = "openai"
		// The omitted store already defaults to file. Keep it omitted so an
		// existing native login remains a byte-for-byte no-op.
		if store == "file" || store == "keyring" || store == "auto" {
			value["cli_auth_credentials_store"] = store
		}
		data, err := marshalCodexTOML(value)
		if err != nil {
			return nil, err
		}
		if reflect.DeepEqual(value, original) {
			data = snapshots[file].data
		}
		auth := []byte("{}\n")
		for _, candidate := range [][]byte{snapshots[authPath].data, savedAuth} {
			if official, _ := codexOfficialConfigurationMatches(data, candidate); official {
				// Keep OAuth but discard a stale API key in a mixed auth file.
				var login map[string]any
				_ = json.Unmarshal(candidate, &login)
				if _, exists := login["OPENAI_API_KEY"]; exists {
					delete(login, "OPENAI_API_KEY")
					auth, err = marshalJSONObject(login)
					if err != nil {
						return nil, err
					}
				} else {
					auth = candidate
				}
				// Keep the selected credentials store. An OAuth file may be
				// stale while keyring holds the current account; auto must
				// retain its keyring-first behavior and file fallback.
				break
			}
		}
		var oldAuth, nextAuth map[string]any
		if json.Unmarshal(snapshots[authPath].data, &oldAuth) == nil && json.Unmarshal(auth, &nextAuth) == nil && reflect.DeepEqual(oldAuth, nextAuth) {
			auth = snapshots[authPath].data
		}
		if bytes.Equal(data, snapshots[file].data) && bytes.Equal(auth, snapshots[authPath].data) {
			return nil, nil
		}
		return []ConfigFileChange{{Path: file, Data: data}, {Path: authPath, Data: auth}}, nil
	}, backupArgs...)
	return result.Rollback, err
}
