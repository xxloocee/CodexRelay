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
	discovered, changed := discoverClientConfigPaths(existing)
	if !changed {
		t.Fatal("missing default config filename should be recorded")
	}
	entry := discovered[config.CategoryCodex]
	if entry.ConfigDir != directory || entry.ConfigFile != "config.toml" {
		t.Fatalf("explicit directory changed unexpectedly: %+v", entry)
	}
}
