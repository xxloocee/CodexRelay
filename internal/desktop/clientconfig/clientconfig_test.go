/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 外部客户端配置适配器的脱敏本地回归测试
 * @File          : 外部客户端配置测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codexrelay/internal/config"
)

func isolatedClientDiscoveryConfig(t *testing.T, proxyPort int) config.AppConfig {
	t.Helper()
	cfg := config.Default(proxyPort)
	root := t.TempDir()
	for _, definition := range clientDefinitions() {
		if definition.Kind == "unsupported" {
			continue
		}
		cfg.ClientConfigs[definition.Category] = config.ClientConfig{
			ConfigDir:  filepath.Join(root, definition.Category),
			ConfigFile: definition.File,
		}
	}
	return cfg
}

func TestConfigureJSONEnvPreservesExistingValuesAndBacksUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte("{\n  \"theme\": \"dark\",\n  \"env\": {\"OTHER\": \"keep\"}\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configureJSONEnv(path, "http://127.0.0.1:8765/codex", "sk-test-placeholder", "BASE_URL", "AUTH_TOKEN"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"\"theme\": \"dark\"", "\"OTHER\": \"keep\"", "\"BASE_URL\": \"http://127.0.0.1:8765/codex\"", "\"AUTH_TOKEN\": \"sk-test-placeholder\""} {
		if !strings.Contains(text, expected) {
			t.Fatalf("updated JSON missing %q: %s", expected, text)
		}
	}
	backups, err := filepath.Glob(path + ".*.CodexRelay")
	if err != nil || len(backups) != 1 {
		t.Fatalf("timestamped backup missing: %v", backups)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(original) {
		t.Fatalf("backup changed original content: %q", backup)
	}
}

func TestConfigureDotEnvPreservesExistingLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("OTHER=value\n# comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configureDotEnv(path, "http://127.0.0.1:8765/gemini", "sk-test-placeholder", "GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY"); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"OTHER=value", "# comment", "GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8765/gemini", "GEMINI_API_KEY=sk-test-placeholder"} {
		if !strings.Contains(string(text), expected) {
			t.Fatalf("updated dotenv missing %q: %s", expected, text)
		}
	}
}

func TestConfigureClaudeAndGeminiWriteSelectedModel(t *testing.T) {
	model := []config.ModelEntry{{ID: "model-a", Name: "Model A"}}
	claudePath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(claudePath, []byte(`{"env":{"KEEP":"yes"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configureClaude(claudePath, "http://127.0.0.1:8765/claude", "sk-test-placeholder", model, "model-a"); err != nil {
		t.Fatal(err)
	}
	claude, err := os.ReadFile(claudePath)
	if err != nil || !strings.Contains(string(claude), `"ANTHROPIC_MODEL": "model-a"`) {
		t.Fatalf("Claude model missing: %v %s", err, claude)
	}
	geminiPath := filepath.Join(t.TempDir(), ".env")
	if err := configureDotEnvWithModel(geminiPath, "http://127.0.0.1:8765/gemini", "sk-test-placeholder", "GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY", model, "model-a"); err != nil {
		t.Fatal(err)
	}
	gemini, err := os.ReadFile(geminiPath)
	if err != nil || !strings.Contains(string(gemini), "GEMINI_MODEL=model-a") {
		t.Fatalf("Gemini model missing: %v %s", err, gemini)
	}
}

func TestBackupIsTimestampedForEveryWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"env":{"KEEP":"one"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configureJSONEnv(path, "http://127.0.0.1:8765/codex", "sk-test-placeholder", "BASE_URL", "AUTH_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if err := configureJSONEnv(path, "http://127.0.0.1:8765/codex", "sk-test-placeholder-2", "BASE_URL", "AUTH_TOKEN"); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(path + ".*.CodexRelay")
	if err != nil || len(backups) != 2 {
		t.Fatalf("expected two timestamped backups, got %v", backups)
	}
}

func TestInspectCodexConfigurationReadsConfigAndAuthSeparately(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	cfg := config.Default(8765)
	cfg.LocalAccessToken = "sk-test-placeholder"
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{ConfigDir: directory, ConfigFile: "config.toml"}
	if err := configureCodex(configPath, filepath.Join(directory, "auth.json"), clientProxyURL(cfg, config.CategoryCodex), cfg.LocalAccessToken, ""); err != nil {
		t.Fatal(err)
	}
	status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryCodex, Label: "Codex", File: "config.toml", Kind: "codex"})
	if !status.Configured || status.Status != clientStatusConfigured {
		t.Fatalf("configured Codex was not detected: %+v", status)
	}
}

func TestConfigureHermesUpsertsOnlyProviderBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "theme: dark\ncustom_providers:\n  - name: other\n    base_url: https://example.test\n  - name: codexrelay\n    base_url: https://old.example\n    request_timeout_seconds: 30\nlogging:\n  level: info\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configureHermes(path, "http://127.0.0.1:8765/hermes", "sk-test-placeholder", []config.ModelEntry{{ID: "model-a", Name: "Model A"}}, "model-a"); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value := string(text)
	for _, expected := range []string{"theme: dark", "name: other", "name: codexrelay", "base_url: http://127.0.0.1:8765/hermes", "api_key: sk-test-placeholder", "request_timeout_seconds: 30", "model-a", "logging:", "level: info"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("updated Hermes YAML missing %q: %s", expected, value)
		}
	}
}

func TestConfigureJSONProviderAdaptersPreserveRoot(t *testing.T) {
	t.Run("OpenCode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "opencode.json")
		if err := os.WriteFile(path, []byte("{\"theme\":\"dark\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := configureOpenCode(path, "http://127.0.0.1:8765/opencode", "sk-test-placeholder", []config.ModelEntry{{ID: "model-a", Name: "Model A"}}, "model-a"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"\"theme\": \"dark\"", "\"codexrelay\"", "\"baseURL\": \"http://127.0.0.1:8765/opencode\"", "\"apiKey\": \"sk-test-placeholder\""} {
			if !strings.Contains(string(data), expected) {
				t.Fatalf("OpenCode config missing %q: %s", expected, data)
			}
		}
	})
	t.Run("OpenClaw", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "openclaw.json")
		if err := os.WriteFile(path, []byte("// OpenClaw JSON5\n{logging: {level: 'info',},}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := configureOpenClaw(path, "http://127.0.0.1:8765/openclaw", "sk-test-placeholder", []config.ModelEntry{{ID: "model-a", Name: "Model A"}}, "model-a"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"\"level\": \"info\"", "\"codexrelay\"", "\"baseUrl\": \"http://127.0.0.1:8765/openclaw\"", "\"apiKey\": \"sk-test-placeholder\""} {
			if !strings.Contains(string(data), expected) {
				t.Fatalf("OpenClaw config missing %q: %s", expected, data)
			}
		}
	})
}

func TestConfigureGrokUpsertsModelSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nname = \"keep\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configureGrok(path, "http://127.0.0.1:8765/grok", "sk-test-placeholder", []config.ModelEntry{{ID: "model-a", Name: "Model A"}}, "model-a"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"name = \"keep\"", "default = \"model-a\"", "[model.\"model-a\"]", "base_url = \"http://127.0.0.1:8765/grok\"", "api_key = \"sk-test-placeholder\"", "api_backend = \"responses\"", "context_window = 500000"} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("Grok config missing %q: %s", expected, data)
		}
	}
}

func TestDiscoverClientConfigPathsKeepsExplicitDirectory(t *testing.T) {
	directory := t.TempDir()
	existing := map[string]config.ClientConfig{
		config.CategoryCodex: {ConfigDir: directory},
	}
	cfg := isolatedClientDiscoveryConfig(t, 18765)
	cfg.ClientConfigs[config.CategoryCodex] = existing[config.CategoryCodex]
	discovered, changed := discoverClientConfigPaths(cfg)
	if !changed {
		t.Fatal("missing default config filename should be recorded")
	}
	entry := discovered[config.CategoryCodex]
	if entry.ConfigDir != directory || entry.ConfigFile != "config.toml" {
		t.Fatalf("explicit directory changed unexpectedly: %+v", entry)
	}
}

func TestLegacyCodexOwnershipMigratesWithoutCurrentEndpointOrToken(t *testing.T) {
	directory := t.TempDir()
	legacy := "model_provider = \"codex_local_access\"\n\n[model_providers.codex_local_access]\nbase_url = \"http://127.0.0.1:9999/codex\"\nwire_api = \"responses\"\nrequires_openai_auth = true\n"
	if err := os.WriteFile(filepath.Join(directory, "config.toml"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "auth.json"), []byte("{\"OPENAI_API_KEY\":\"old-relay-token\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := isolatedClientDiscoveryConfig(t, 18765)
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{ConfigDir: directory, ConfigFile: "config.toml"}
	discovered, changed := discoverClientConfigPaths(cfg)
	if !changed || discovered[config.CategoryCodex].Mode != "relay" {
		t.Fatalf("legacy Relay ownership was not migrated: changed=%v config=%+v", changed, discovered[config.CategoryCodex])
	}
}

func TestConfigureLegacyCodexWithoutSnapshotDoesNotCreateOfficialBackup(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	authPath := filepath.Join(directory, "auth.json")
	legacy := "model_provider = \"codex_local_access\"\n\n[model_providers.codex_local_access]\nbase_url = \"http://127.0.0.1:9999/codex\"\nwire_api = \"responses\"\nrequires_openai_auth = true\n"
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte("{\"OPENAI_API_KEY\":\"old-relay-token\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{ConfigDir: directory, ConfigFile: "config.toml"}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	result, err := ConfigureWithResult(cfg, config.CategoryCodex, "profile-b", dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if result.OfficialSnapshot {
		t.Fatal("legacy Relay content must not become an official snapshot")
	}
	for _, file := range result.Files {
		if file.BackupPath != "" {
			t.Fatalf("legacy Relay file was backed up as official: %+v", file)
		}
	}
	configData, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(configData), `model_provider = "codexrelay"`) {
		t.Fatalf("legacy provider was not updated: error=%v config=%s", err, configData)
	}
	authData, err := os.ReadFile(authPath)
	if err != nil || !strings.Contains(string(authData), `"OPENAI_API_KEY": "new-local-token"`) {
		t.Fatalf("local token was not updated: error=%v auth=%s", err, authData)
	}
	status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryCodex, Label: "Codex", File: "config.toml", Kind: "codex"})
	if status.ConfigState != ClientConfigStateManagedWithoutSnapshot {
		t.Fatalf("rewritten legacy state = %q, want %q", status.ConfigState, ClientConfigStateManagedWithoutSnapshot)
	}
}

func TestRelayModeDoesNotOverrideUnknownDiskConfiguration(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "settings.json")
	if err := os.WriteFile(path, []byte("{\"env\":{\"OTHER\":\"keep\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
	}
	status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryClaude, Label: "Claude", File: "settings.json", Kind: "claude"})
	if status.ConfigState != ClientConfigStateUnmanaged {
		t.Fatalf("unknown disk configuration state = %q, want %q", status.ConfigState, ClientConfigStateUnmanaged)
	}
}

func TestCodexOwnershipRequiresCompleteRelayConfiguration(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	authPath := filepath.Join(directory, "auth.json")
	validConfig := "model_provider = \"codexrelay\"\n\n[model_providers.codexrelay]\nbase_url = \"http://127.0.0.1:9999/codex\"\nwire_api = \"responses\"\nrequires_openai_auth = true\n"

	tests := []struct {
		name   string
		config string
		auth   string
		owned  bool
	}{
		{name: "relay", config: validConfig, auth: "{\"OPENAI_API_KEY\":\"old-relay-token\"}\n", owned: true},
		{name: "oauth mixed with relay provider", config: validConfig, auth: "{\"auth_mode\":\"chatgpt\",\"tokens\":{\"account_id\":\"acct-test\",\"access_token\":\"oauth-placeholder\"}}\n"},
		{name: "provider name without relay section", config: "model_provider = \"codexrelay\"\n", auth: "{\"OPENAI_API_KEY\":\"old-relay-token\"}\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(configPath, []byte(test.config), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(authPath, []byte(test.auth), 0o600); err != nil {
				t.Fatal(err)
			}
			owned, err := clientConfigurationOwnedByRelay(clientDefinition{Category: config.CategoryCodex, Kind: "codex"}, directory, configPath)
			if err != nil {
				t.Fatal(err)
			}
			if owned != test.owned {
				t.Fatalf("owned = %v, want %v", owned, test.owned)
			}
		})
	}
}

func TestLegacyClaudeAndGeminiOwnershipMigrates(t *testing.T) {
	claudeDirectory := t.TempDir()
	geminiDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(claudeDirectory, "settings.json"), []byte("{\"env\":{\"ANTHROPIC_BASE_URL\":\"http://127.0.0.1:9999/claude\",\"ANTHROPIC_AUTH_TOKEN\":\"old-relay-token\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(geminiDirectory, ".env"), []byte("GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:9999/gemini\nGEMINI_API_KEY=old-relay-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := isolatedClientDiscoveryConfig(t, 9999)
	cfg.LocalAccessToken = "old-relay-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{ConfigDir: claudeDirectory, ConfigFile: "settings.json"}
	cfg.ClientConfigs[config.CategoryGemini] = config.ClientConfig{ConfigDir: geminiDirectory, ConfigFile: ".env"}
	discovered, changed := discoverClientConfigPaths(cfg)
	if !changed {
		t.Fatal("legacy ownership migration did not report a change")
	}
	for _, category := range []string{config.CategoryClaude, config.CategoryGemini} {
		if discovered[category].Mode != "relay" {
			t.Fatalf("%s legacy ownership was not migrated: %+v", category, discovered[category])
		}
	}
}

func TestLegacyClaudeAndGeminiOwnershipRequiresSavedEndpointAndToken(t *testing.T) {
	claudeDirectory := t.TempDir()
	geminiDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(claudeDirectory, "settings.json"), []byte("{\"env\":{\"ANTHROPIC_BASE_URL\":\"http://127.0.0.1:9999/claude\",\"ANTHROPIC_AUTH_TOKEN\":\"unrelated-token\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(geminiDirectory, ".env"), []byte("GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:9999/gemini\nGEMINI_API_KEY=unrelated-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := isolatedClientDiscoveryConfig(t, 18765)
	cfg.LocalAccessToken = "saved-relay-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{ConfigDir: claudeDirectory, ConfigFile: "settings.json"}
	cfg.ClientConfigs[config.CategoryGemini] = config.ClientConfig{ConfigDir: geminiDirectory, ConfigFile: ".env"}
	discovered, changed := discoverClientConfigPaths(cfg)
	if changed {
		t.Fatalf("unrelated local gateways must not migrate: %+v", discovered)
	}
	for _, category := range []string{config.CategoryClaude, config.CategoryGemini} {
		if discovered[category].Mode != "" {
			t.Fatalf("%s unrelated gateway was marked as Relay-owned: %+v", category, discovered[category])
		}
	}
}

func TestConfigureMissingManagedClientWithoutSnapshotKeepsRestoreUnavailable(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
	}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	result, err := ConfigureWithResult(cfg, config.CategoryClaude, "profile-b", dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if result.OfficialSnapshot {
		t.Fatal("rebuilding a managed client without a snapshot must not create an official snapshot")
	}
	for _, file := range result.Files {
		if file.BackupPath != "" {
			t.Fatalf("missing managed file unexpectedly created an official backup: %+v", file)
		}
	}
}

func TestExplicitTakeoverReplacesStaleSnapshotForUnknownConfiguration(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	path := filepath.Join(directory, "settings.json")
	original := []byte("{\"env\":{\"OTHER\":\"external\"}}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
		OfficialBackups: []config.ClientConfigBackup{{Path: path, Existed: false, ExpectedSHA256: "stale-placeholder"}},
	}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	result, err := ConfigureWithResult(cfg, config.CategoryClaude, "profile-b", dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ResetOfficialSnapshot {
		t.Fatal("explicit takeover of unknown content must replace stale snapshot metadata")
	}
	if len(result.Files) != 1 || result.Files[0].BackupPath == "" {
		t.Fatalf("explicit takeover did not capture the current external configuration: %+v", result.Files)
	}
	backup, err := os.ReadFile(result.Files[0].BackupPath)
	if err != nil || string(backup) != string(original) {
		t.Fatalf("fresh official backup mismatch: error=%v data=%q", err, backup)
	}
}

func TestManagedUpdateRejectsUnknownConfigurationWithoutChangingIt(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	path := filepath.Join(directory, "settings.json")
	original := []byte("{\"env\":{\"OTHER\":\"external\"}}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
		OfficialBackups: []config.ClientConfigBackup{{Path: path, Existed: false, ExpectedSHA256: "stale-placeholder"}},
	}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	if _, err := UpdateManagedWithResult(cfg, config.CategoryClaude, "profile-b", dataDirectory); err == nil || !strings.Contains(err.Error(), "拒绝自动覆盖") {
		t.Fatalf("managed update should reject unknown content: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(original) {
		t.Fatalf("rejected managed update changed the client file: error=%v data=%q", err, current)
	}
}

func TestManagedUpdateRejectsDuplicateOfficialBackupPaths(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	path := filepath.Join(directory, "settings.json")
	relayData := []byte("{\"env\":{\"ANTHROPIC_BASE_URL\":\"http://127.0.0.1:9999/claude\",\"ANTHROPIC_AUTH_TOKEN\":\"old-relay-token\"}}\n")
	if err := os.WriteFile(path, relayData, 0o600); err != nil {
		t.Fatal(err)
	}
	backup := config.ClientConfigBackup{Path: path, Existed: false, ExpectedSHA256: sha256Hex(relayData)}
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
		OfficialBackups: []config.ClientConfigBackup{backup, backup},
	}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	if _, err := UpdateManagedWithResult(cfg, config.CategoryClaude, "profile-b", dataDirectory); err == nil || !strings.Contains(err.Error(), "路径重复") {
		t.Fatalf("duplicate official backup paths should be rejected: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(relayData) {
		t.Fatalf("invalid metadata changed client content: error=%v data=%q", err, current)
	}
}

func TestManagedUpdateRejectsConfigurationChangedAfterInspect(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	path := filepath.Join(directory, "settings.json")
	relayData := []byte("{\"env\":{\"ANTHROPIC_BASE_URL\":\"http://127.0.0.1:9999/claude\",\"ANTHROPIC_AUTH_TOKEN\":\"old-relay-token\"}}\n")
	if err := os.WriteFile(path, relayData, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
		OfficialBackups: []config.ClientConfigBackup{{Path: path, Existed: false, ExpectedSHA256: sha256Hex(relayData)}},
	}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryClaude, Label: "Claude", File: "settings.json", Kind: "claude"})
	if status.ConfigState != ClientConfigStateManaged {
		t.Fatalf("precondition state = %q, want %q", status.ConfigState, ClientConfigStateManaged)
	}
	external := []byte("{\"env\":{\"OTHER\":\"changed-after-inspect\"}}\n")
	if err := os.WriteFile(path, external, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateManagedWithResult(cfg, config.CategoryClaude, "profile-b", dataDirectory); err == nil || !strings.Contains(err.Error(), "拒绝自动覆盖") {
		t.Fatalf("post-inspect external change should be rejected: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(external) {
		t.Fatalf("post-inspect external change was overwritten: error=%v data=%q", err, current)
	}
}

func TestInspectAndManagedUpdateRejectFingerprintMismatchWithRelayStructure(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	path := filepath.Join(directory, "settings.json")
	relayData := []byte("{\"env\":{\"ANTHROPIC_BASE_URL\":\"http://127.0.0.1:9999/claude\",\"ANTHROPIC_AUTH_TOKEN\":\"externally-changed-token\"}}\n")
	if err := os.WriteFile(path, relayData, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
		OfficialBackups: []config.ClientConfigBackup{{Path: path, Existed: false, ExpectedSHA256: sha256Hex([]byte("previous-relay-generation"))}},
	}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryClaude, Label: "Claude", File: "settings.json", Kind: "claude"})
	if status.ConfigState != ClientConfigStateUnmanaged || status.Configured {
		t.Fatalf("fingerprint mismatch must be exposed as unmanaged: %+v", status)
	}
	if _, err := UpdateManagedWithResult(cfg, config.CategoryClaude, "profile-b", dataDirectory); err == nil || !strings.Contains(err.Error(), "拒绝自动覆盖") {
		t.Fatalf("managed update should reject a structurally Relay-like fingerprint mismatch: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(relayData) {
		t.Fatalf("rejected update changed external content: error=%v data=%q", err, current)
	}
	result, err := ConfigureWithResult(cfg, config.CategoryClaude, "profile-b", dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResetOfficialSnapshot || len(result.Files) != 1 || result.Files[0].BackupPath != "" {
		t.Fatalf("confirmed Relay update replaced the original official baseline: %+v", result)
	}
}

func TestInspectUsesCompleteSavedFingerprintsForManagedFallback(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "settings.json")
	legacyData := []byte("{\"legacy_relay_format\":true}\n")
	if err := os.WriteFile(path, legacyData, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
		OfficialBackups: []config.ClientConfigBackup{{Path: path, Existed: false, ExpectedSHA256: sha256Hex(legacyData)}},
	}
	status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryClaude, Label: "Claude", File: "settings.json", Kind: "claude"})
	if status.ConfigState != ClientConfigStateManaged {
		t.Fatalf("fingerprint-matched configuration state = %q, want %q", status.ConfigState, ClientConfigStateManaged)
	}
}

func TestManagedUpdateRebuildsMissingClientWithoutCreatingSnapshot(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "relay",
	}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	result, err := UpdateManagedWithResult(cfg, config.CategoryClaude, "profile-b", dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if result.OfficialSnapshot {
		t.Fatal("managed rebuild without an original snapshot must keep official restore unavailable")
	}
	data, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil || !strings.Contains(string(data), "new-local-token") {
		t.Fatalf("managed rebuild did not restore Relay configuration: error=%v data=%q", err, data)
	}
}

func TestManagedUpdateRejectsCodexOAuthRelayMixedState(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	authPath := filepath.Join(directory, "auth.json")
	configData := []byte("model_provider = \"codexrelay\"\n\n[model_providers.codexrelay]\nbase_url = \"http://127.0.0.1:9999/codex\"\nwire_api = \"responses\"\nrequires_openai_auth = true\n")
	authData := []byte("{\"auth_mode\":\"chatgpt\",\"tokens\":{\"account_id\":\"acct-test\",\"access_token\":\"oauth-placeholder\"}}\n")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, authData, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{ConfigDir: directory, ConfigFile: "config.toml", Mode: "relay"}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	if _, err := UpdateManagedWithResult(cfg, config.CategoryCodex, "profile-b", dataDirectory); err == nil || !strings.Contains(err.Error(), "拒绝自动覆盖") {
		t.Fatalf("mixed OAuth/Relay state should be rejected: %v", err)
	}
	if _, err := ConfigureWithResult(cfg, config.CategoryCodex, "profile-b", dataDirectory); err == nil || !strings.Contains(err.Error(), "混合配置") {
		t.Fatalf("explicit takeover must not snapshot a mixed Codex state: %v", err)
	}
	currentAuth, err := os.ReadFile(authPath)
	if err != nil || string(currentAuth) != string(authData) {
		t.Fatalf("mixed OAuth auth.json was overwritten: error=%v data=%q", err, currentAuth)
	}
}

func TestCodexMissingManagedTargetsReuseOfficialSnapshot(t *testing.T) {
	for _, missing := range []string{"config.toml", "auth.json"} {
		t.Run(missing, func(t *testing.T) {
			directory := t.TempDir()
			dataDirectory := t.TempDir()
			configPath := filepath.Join(directory, "config.toml")
			authPath := filepath.Join(directory, "auth.json")
			configData := []byte("model_provider = \"codexrelay\"\n\n[model_providers.codexrelay]\nbase_url = \"http://127.0.0.1:9999/codex\"\nwire_api = \"responses\"\nrequires_openai_auth = true\n")
			authData := []byte("{\"OPENAI_API_KEY\":\"old-relay-token\"}\n")
			if err := os.WriteFile(configPath, configData, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(authPath, authData, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default(18765)
			cfg.LocalAccessToken = "new-local-token"
			cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{
				ConfigDir: directory, ConfigFile: "config.toml", Mode: "relay",
				OfficialBackups: []config.ClientConfigBackup{
					{Path: configPath, Existed: false, ExpectedSHA256: sha256Hex(configData)},
					{Path: authPath, Existed: false, ExpectedSHA256: sha256Hex(authData)},
				},
			}
			cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
			if err := os.Remove(filepath.Join(directory, missing)); err != nil {
				t.Fatal(err)
			}
			status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryCodex, Label: "Codex", File: "config.toml", Kind: "codex"})
			if status.ConfigState != ClientConfigStateManaged {
				t.Fatalf("partial missing Codex state = %+v", status)
			}
			result, err := UpdateManagedWithResult(cfg, config.CategoryCodex, "profile-b", dataDirectory)
			if err != nil {
				t.Fatal(err)
			}
			if result.ResetOfficialSnapshot || len(result.Files) != 2 {
				t.Fatalf("partial Codex rebuild replaced its official lineage: %+v", result)
			}
			if _, err := os.Stat(filepath.Join(directory, missing)); err != nil {
				t.Fatalf("missing Codex target was not rebuilt: %v", err)
			}
		})
	}
}

func TestGeminiMissingManagedTargetsReuseOfficialSnapshot(t *testing.T) {
	for _, missing := range []string{".env", "settings.json"} {
		t.Run(missing, func(t *testing.T) {
			directory := t.TempDir()
			dataDirectory := t.TempDir()
			envPath := filepath.Join(directory, ".env")
			settingsPath := filepath.Join(directory, "settings.json")
			envData := []byte("GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:9999/gemini\nGEMINI_API_KEY=old-relay-token\n")
			settingsData := []byte("{\"security\":{\"auth\":{\"selectedType\":\"gemini-api-key\"}}}\n")
			officialEnv := []byte("GEMINI_API_KEY=official-key\n")
			officialSettings := []byte("{\"security\":{\"auth\":{\"selectedType\":\"oauth-personal\"}}}\n")
			if err := os.WriteFile(envPath, envData, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(settingsPath, settingsData, 0o600); err != nil {
				t.Fatal(err)
			}
			backupDirectory := ClientBackupDirectory(dataDirectory, config.CategoryGemini)
			if err := os.MkdirAll(backupDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			envBackupName := "env.CodexRelay"
			settingsBackupName := "settings.CodexRelay"
			if err := os.WriteFile(filepath.Join(backupDirectory, envBackupName), officialEnv, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(backupDirectory, settingsBackupName), officialSettings, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default(18765)
			cfg.LocalAccessToken = "new-local-token"
			cfg.ClientConfigs[config.CategoryGemini] = config.ClientConfig{
				ConfigDir: directory, ConfigFile: ".env", Mode: "relay",
				OfficialBackups: []config.ClientConfigBackup{
					{Path: envPath, BackupPath: filepath.Join("client-backups", config.CategoryGemini, envBackupName), BackupSHA256: sha256Hex(officialEnv), Existed: true, ExpectedSHA256: sha256Hex(envData)},
					{Path: settingsPath, BackupPath: filepath.Join("client-backups", config.CategoryGemini, settingsBackupName), BackupSHA256: sha256Hex(officialSettings), Existed: true, ExpectedSHA256: sha256Hex(settingsData)},
				},
			}
			cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryGemini, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
			if err := os.Remove(filepath.Join(directory, missing)); err != nil {
				t.Fatal(err)
			}
			status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryGemini, Label: "Gemini", File: ".env", Kind: "gemini"})
			if status.ConfigState != ClientConfigStateManaged {
				t.Fatalf("partial missing Gemini state = %+v", status)
			}
			result, err := UpdateManagedWithResult(cfg, config.CategoryGemini, "profile-b", dataDirectory)
			if err != nil {
				t.Fatal(err)
			}
			if result.ResetOfficialSnapshot || len(result.Files) != 2 {
				t.Fatalf("partial Gemini rebuild replaced its official lineage: %+v", result)
			}
			if _, err := os.Stat(filepath.Join(directory, missing)); err != nil {
				t.Fatalf("missing Gemini target was not rebuilt: %v", err)
			}
		})
	}
}

func TestInspectDoesNotLetOfficialModeHideRelayFingerprintConflict(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "settings.json")
	relayData := []byte("{\"env\":{\"ANTHROPIC_BASE_URL\":\"http://127.0.0.1:9999/claude\",\"ANTHROPIC_AUTH_TOKEN\":\"changed-token\"}}\n")
	if err := os.WriteFile(path, relayData, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{
		ConfigDir: directory, ConfigFile: "settings.json", Mode: "official",
		OfficialBackups: []config.ClientConfigBackup{{Path: path, Existed: false, ExpectedSHA256: "previous-relay-sha"}},
	}
	status := inspectClientConfig(cfg, clientDefinition{Category: config.CategoryClaude, Label: "Claude", File: "settings.json", Kind: "claude"})
	if status.ConfigState != ClientConfigStateUnmanaged || status.Configured {
		t.Fatalf("stale official mode hid a Relay fingerprint conflict: %+v", status)
	}
}

func TestExplicitTakeoverRejectsPartialGeminiRelayState(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := t.TempDir()
	envPath := filepath.Join(directory, ".env")
	settingsPath := filepath.Join(directory, "settings.json")
	envData := []byte("GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:9999/gemini\nGEMINI_API_KEY=old-relay-token\n")
	settingsData := []byte("{\"security\":{\"auth\":{\"selectedType\":\"oauth-personal\"}}}\n")
	if err := os.WriteFile(envPath, envData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, settingsData, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "new-local-token"
	cfg.ClientConfigs[config.CategoryGemini] = config.ClientConfig{ConfigDir: directory, ConfigFile: ".env", Mode: "relay"}
	cfg.Profiles = []config.Profile{{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryGemini, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"}}
	if _, err := ConfigureWithResult(cfg, config.CategoryGemini, "profile-b", dataDirectory); err == nil || !strings.Contains(err.Error(), "混合配置") {
		t.Fatalf("explicit takeover must not snapshot a partial Gemini state: %v", err)
	}
	currentEnv, envErr := os.ReadFile(envPath)
	currentSettings, settingsErr := os.ReadFile(settingsPath)
	if envErr != nil || settingsErr != nil || string(currentEnv) != string(envData) || string(currentSettings) != string(settingsData) {
		t.Fatalf("rejected takeover changed Gemini files: envErr=%v settingsErr=%v env=%q settings=%q", envErr, settingsErr, currentEnv, currentSettings)
	}
}
