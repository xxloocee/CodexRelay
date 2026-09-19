package clientconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"

	"codexrelay/internal/config"
)

// No free-form config values, secrets, account identifiers or parser errors.
type CodexConfigDiagnosis struct {
	Mode                string `json:"mode"`
	Directory           string `json:"directory"`
	DirectorySource     string `json:"directorySource"`
	Provider            string `json:"provider"`
	AuthSource          string `json:"authSource"`
	ExpectedURL         string `json:"expectedURL"`
	ValidTOML           bool   `json:"validTOML"`
	UniqueProvider      bool   `json:"uniqueProvider"`
	ProviderFieldsMatch bool   `json:"providerFieldsMatch"`
	AuthMatches         bool   `json:"authMatches"`
	Configured          bool   `json:"configured"`
	BackupCount         int    `json:"backupCount"`
}

func DiagnoseCodex(cfg config.AppConfig) CodexConfigDiagnosis {
	entry := cfg.ClientConfigs[config.CategoryCodex]
	dir, source := ResolveCodexDirectory(entry)
	d := CodexConfigDiagnosis{Directory: dir, DirectorySource: source, ExpectedURL: clientProxyURL(cfg, config.CategoryCodex), Provider: "unknown", AuthSource: "unknown", BackupCount: len(entry.OfficialBackups)}
	d.Mode = "relay"
	if cfg.ActiveProfiles[config.CategoryCodex] == "" {
		d.Mode = "official"
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		return d
	}
	value, err := readCodexTOML(data)
	if err != nil {
		return d
	}
	d.ValidTOML = true
	store := codexCredentialStore(value)
	switch store {
	case "keyring":
		d.AuthSource = "keyring"
	case "auto":
		d.AuthSource = "auto"
	}
	switch provider := codexSelectedProvider(value); provider {
	case "openai", codexRelayModelProviderID, codexLegacyModelProviderID:
		d.Provider = provider
	default:
		d.Provider = "custom"
	}
	// UniqueProvider describes the selected channel, not the number of saved
	// provider definitions. Unused providers and display names are allowed.
	d.UniqueProvider = codexSelectedProvider(value) == codexRelayModelProviderID
	if canonical, renderErr := renderCodexRelayTOML(data, d.ExpectedURL, ""); renderErr == nil {
		expected, _ := readCodexTOML(canonical)
		d.ProviderFieldsMatch = reflect.DeepEqual(value, expected)
	}
	authData, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil && !os.IsNotExist(err) {
		return d
	}
	var auth map[string]any
	if len(authData) > 0 && json.Unmarshal(authData, &auth) != nil {
		return d
	}
	d.AuthSource = "file_other"
	if stringField(auth, "auth_mode") == "chatgpt" {
		d.AuthSource = "file_oauth"
	}
	if stringField(auth, "OPENAI_API_KEY") != "" {
		d.AuthSource = "file_api_key"
	}
	switch store {
	case "keyring":
		d.AuthSource = "keyring"
	case "auto":
		d.AuthSource = "auto"
	}
	d.AuthMatches = store == "file" && cfg.LocalAccessToken != "" && len(auth) == 1 && stringField(auth, "OPENAI_API_KEY") == cfg.LocalAccessToken
	d.Configured, _ = codexRelayConfigurationMatches(data, authData, d.ExpectedURL, cfg.LocalAccessToken, "", false)
	if d.Mode == "official" {
		d.ExpectedURL = ""
		d.UniqueProvider = codexSelectedProvider(value) == "openai"
		expected, _ := readCodexTOML(data)
		setCodexNativeConnection(expected, store)
		d.ProviderFieldsMatch = reflect.DeepEqual(value, expected)
		_, hasAPIKey := auth["OPENAI_API_KEY"]
		oauth, _ := codexOfficialConfigurationMatches(data, authData)
		// An empty auth file is the supported logged-out official state.
		// Keyring/auto may hold the login outside auth.json.
		d.AuthMatches = !hasAPIKey && (len(auth) == 0 || oauth)
		d.Configured = d.UniqueProvider && d.ProviderFieldsMatch && d.AuthMatches
	}
	return d
}
