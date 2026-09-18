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
	switch stringField(value, "cli_auth_credentials_store") {
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
	providers, _ := value["model_providers"].(map[string]any)
	d.UniqueProvider = len(providers) == 1 && providers[codexRelayModelProviderID] != nil
	d.ProviderFieldsMatch = reflect.DeepEqual(providers[codexRelayModelProviderID], codexRelayProvider(d.ExpectedURL))
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
	switch stringField(value, "cli_auth_credentials_store") {
	case "keyring":
		d.AuthSource = "keyring"
	case "auto":
		d.AuthSource = "auto"
	}
	d.AuthMatches = len(auth) == 1 && stringField(auth, "OPENAI_API_KEY") == cfg.LocalAccessToken
	d.Configured, _ = codexRelayConfigurationMatches(data, authData, d.ExpectedURL, cfg.LocalAccessToken, "", false)
	if d.Mode == "official" {
		d.ExpectedURL = ""
		d.UniqueProvider = len(providers) <= 1 && (len(providers) == 0 && d.Provider == "openai" || len(providers) == 1 && providers[codexSelectedProvider(value)] != nil)
		canonical, cleanAuth, canonicalErr := renderCodexOfficialData(data, authData)
		if canonicalErr == nil {
			expected, _ := readCodexTOML(canonical)
			var expectedAuth map[string]any
			_ = json.Unmarshal(cleanAuth, &expectedAuth)
			d.ProviderFieldsMatch = reflect.DeepEqual(value, expected)
			d.AuthMatches = reflect.DeepEqual(auth, expectedAuth)
			d.Configured = d.UniqueProvider && d.ProviderFieldsMatch && d.AuthMatches
		} else {
			d.Configured = false
		}
	}
	return d
}
