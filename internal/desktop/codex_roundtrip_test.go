package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"codexrelay/internal/config"
)

// All credentials are synthetic; every client target lives under TempDir.
func TestCodexOAuthAPIRoundTripPreservesLogin(t *testing.T) {
	for _, drift := range []string{"none", "external-edit", "broken-auth", "missing-auth"} {
		t.Run(drift, func(t *testing.T) {
			directory, clientDir := t.TempDir(), t.TempDir()
			configPath, authPath := filepath.Join(clientDir, "config.toml"), filepath.Join(clientDir, "auth.json")
			officialConfig := "  model_provider = \"openai\" # official\n  model = \"gpt-5\"\n[model_providers.codexrelay] # retained provider\nbase_url = \"http://127.0.0.1:9999/codex\"\n"
			officialAuth := `{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":{"account_id":"account-placeholder","access_token":"access-placeholder","refresh_token":"refresh-placeholder","id_token":"id-placeholder"},"last_refresh":"2026-09-17T00:00:00Z"}`
			write := func(path, data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			read := func(path string) string {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			write(configPath, officialConfig)
			write(authPath, officialAuth)
			cfg := config.Default(18765)
			// Prevent discovery from reading any real installed client.
			for _, category := range config.Categories {
				entry := cfg.ClientConfigs[category]
				entry.ConfigDir = filepath.Join(t.TempDir(), category)
				cfg.ClientConfigs[category] = entry
			}
			cfg.LocalAccessToken = "sk-local-placeholder"
			cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{ConfigDir: clientDir, ConfigFile: "config.toml"}
			cfg.Profiles = []config.Profile{
				{ID: "api-a", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "A", BaseURL: "https://a.example/v1", APIKey: "sk-a-placeholder", Models: []config.ModelEntry{{ID: "gpt-5"}}, DefaultModel: "gpt-5"},
				{ID: "api-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b-placeholder"},
			}
			store := config.NewStore(filepath.Join(directory, "config.json"))
			if err := store.Save(cfg); err != nil {
				t.Fatal(err)
			}
			service := NewDesktopService(newTestRuntime(t, directory, store, cfg))
			assertAPI := func(id string) {
				t.Helper()
				var auth map[string]any
				if err := json.Unmarshal([]byte(read(authPath)), &auth); err != nil {
					t.Fatal(err)
				}
				if len(auth) != 1 || auth["OPENAI_API_KEY"] != cfg.LocalAccessToken {
					t.Fatal("API mode must use only the local Relay key")
				}
				text := read(configPath)
				if !strings.Contains(text, `model_provider = "codexrelay"`) || !strings.Contains(text, `base_url = "http://127.0.0.1:18765/codex"`) || strings.Count(text, "[model_providers.codexrelay]") != 1 {
					t.Fatal("Relay provider was not written correctly")
				}
				if service.runtime.State().Config.ActiveProfiles[config.CategoryCodex] != id {
					t.Fatal("wrong active upstream")
				}
			}
			for cycle := 0; cycle < 2; cycle++ {
				if err := service.ActivateProfile("api-a", true, true); err != nil {
					t.Fatal(err)
				}
				assertAPI("api-a")
				// Reload persisted metadata as a fresh process would before switching.
				var persisted config.AppConfig
				if err := json.Unmarshal([]byte(read(store.Path())), &persisted); err != nil {
					t.Fatal(err)
				}
				service = NewDesktopService(newTestRuntime(t, directory, store, persisted))
				applyDrift := func() {
					switch drift {
					case "external-edit":
						write(configPath, read(configPath)+"\n# external edit\n")
					case "broken-auth":
						write(authPath, "{broken")
					case "missing-auth":
						if err := os.Remove(authPath); err != nil {
							t.Fatal(err)
						}
					}
				}
				applyDrift()
				if err := service.activateProfileFromTray("api-b"); err != nil {
					t.Fatal(err)
				}
				assertAPI("api-b")
				applyDrift()
				if err := service.ActivateProfile("official:codex"); err != nil {
					t.Fatal(err)
				}
				if !codexOfficialRoundTripMatches(t, read(configPath), read(authPath), officialAuth) {
					t.Fatal("official configuration or OAuth login changed during round trip")
				}
				state := service.runtime.State().Config
				if state.ActiveProfiles[config.CategoryCodex] != "" || state.ClientConfigs[config.CategoryCodex].Mode != "official" {
					t.Fatal("official switch retained the Relay route")
				}
				// Simulate OAuth refresh between cycles; the next takeover must preserve it.
				officialAuth = strings.ReplaceAll(officialAuth, "refresh-placeholder", "refreshed-placeholder")
				write(authPath, officialAuth)
			}
			// A fresh external OAuth login must replace a stale official snapshot.
			if err := service.ActivateProfile("api-a", true, true); err != nil {
				t.Fatal(err)
			}
			officialAuth = strings.ReplaceAll(officialAuth, "account-placeholder", "new-account-placeholder")
			write(configPath, officialConfig)
			write(authPath, officialAuth)
			if err := service.ActivateProfile("api-b", true, true); err != nil {
				t.Fatal(err)
			}
			assertAPI("api-b")
			if err := service.ActivateProfile("official:codex"); err != nil {
				t.Fatal(err)
			}
			if !codexOfficialRoundTripMatches(t, read(configPath), read(authPath), officialAuth) {
				t.Fatal("fresh external OAuth login was replaced by a stale snapshot")
			}
		})
	}
}

// Native selection may retain inactive providers, but must use built-in OpenAI.
func codexOfficialRoundTripMatches(t *testing.T, configText, authText, originalAuth string) bool {
	t.Helper()
	var cfg map[string]any
	var auth, expected map[string]any
	if err := toml.Unmarshal([]byte(configText), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(authText), &auth); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(originalAuth), &expected); err != nil {
		t.Fatal(err)
	}
	delete(expected, "OPENAI_API_KEY")
	providers, _ := cfg["model_providers"].(map[string]any)
	return cfg["model_provider"] == "openai" && cfg["model"] == "gpt-5" && providers["openai"] == nil && reflect.DeepEqual(auth, expected)
}
