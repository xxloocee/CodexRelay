/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 外部 AI 客户端路径探测、接管状态检查与受控写入
 * @File          : 外部客户端配置适配器兼容实现
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codexrelay/internal/config"
)

const (
	clientStatusConfigured    = "configured"
	clientStatusNotConfigured = "not_configured"
	clientStatusNotDetected   = "not_detected"
	clientStatusUnsupported   = "unsupported"
	clientStatusError         = "error"
)

// PublicClientConfig 是高级设置和启用前检查使用的脱敏状态，不返回外部配置正文。
type PublicClientConfig struct {
	Category              string `json:"category"`
	Label                 string `json:"label"`
	ConfigDir             string `json:"configDir"`
	ConfigFile            string `json:"configFile"`
	SkipConfigReplacement bool   `json:"skipConfigReplacement"`
	Status                string `json:"status"`
	Detected              bool   `json:"detected"`
	Configured            bool   `json:"configured"`
	RequiresProfile       bool   `json:"requiresProfile"`
	StatusText            string `json:"statusText"`
	LastChecked           string `json:"lastChecked"`
	Error                 string `json:"error,omitempty"`
}

type clientDefinition struct {
	Category        string
	Label           string
	File            string
	Kind            string
	Default         func() string
	RequiresProfile bool
}

func clientDefinitions() []clientDefinition {
	home, _ := os.UserHomeDir()
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" && home != "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	hermesHome := strings.TrimSpace(os.Getenv("HERMES_HOME"))
	if hermesHome == "" {
		hermesHome = filepath.Join(localAppData, "hermes")
	}
	return []clientDefinition{
		{Category: config.CategoryCodex, Label: "Codex", File: "config.toml", Kind: "codex", Default: func() string { return filepath.Join(home, ".codex") }},
		{Category: config.CategoryClaude, Label: "Claude", File: "settings.json", Kind: "claude", Default: func() string { return filepath.Join(home, ".claude") }},
		{Category: config.CategoryGemini, Label: "Gemini", File: ".env", Kind: "gemini", Default: func() string { return filepath.Join(home, ".gemini") }},
		{Category: config.CategoryGrok, Label: "Grok", File: "config.toml", Kind: "grok", Default: func() string { return filepath.Join(home, ".grok") }, RequiresProfile: true},
		{Category: config.CategoryOpenCode, Label: "OpenCode", File: "opencode.json", Kind: "opencode", Default: func() string { return filepath.Join(home, ".config", "opencode") }},
		{Category: config.CategoryOpenClaw, Label: "OpenClaw", File: "openclaw.json", Kind: "openclaw", Default: func() string { return filepath.Join(home, ".openclaw") }},
		{Category: config.CategoryHermes, Label: "Hermes", File: "config.yaml", Kind: "hermes", Default: func() string { return hermesHome }},
		{Category: config.CategoryImage, Label: "生图", Kind: "unsupported", Default: func() string { return "" }},
		{Category: config.CategoryOther, Label: "其他", Kind: "unsupported", Default: func() string { return "" }},
	}
}

func clientDefinitionFor(category string) (clientDefinition, bool) {
	for _, definition := range clientDefinitions() {
		if definition.Category == category {
			return definition, true
		}
	}
	return clientDefinition{}, false
}

func clientConfigPath(definition clientDefinition, entry config.ClientConfig) (string, string) {
	directory := strings.TrimSpace(entry.ConfigDir)
	if directory == "" {
		directory = definition.Default()
	}
	filename := strings.TrimSpace(entry.ConfigFile)
	// The adapter owns the filename; a user-selected directory is the only
	// configurable target. Ignore stale/invalid names from older configs.
	if expected, supported := config.ClientConfigFileFor(definition.Category); supported {
		filename = expected
	} else if filename == "" {
		filename = definition.File
	}
	if directory == "" || filename == "" {
		return directory, ""
	}
	return directory, filepath.Join(directory, filename)
}

// discoverClientConfigPaths 只检查各软件已知默认目录，不遍历磁盘；已有自定义路径永远优先保留。
func discoverClientConfigPaths(existing map[string]config.ClientConfig) (map[string]config.ClientConfig, bool) {
	result := make(map[string]config.ClientConfig, len(existing)+len(clientDefinitions()))
	for category, value := range existing {
		result[category] = value
	}
	changed := false
	for _, definition := range clientDefinitions() {
		entry := result[definition.Category]
		if strings.TrimSpace(entry.ConfigDir) != "" || definition.Kind == "unsupported" {
			if entry.ConfigFile == "" && definition.File != "" {
				entry.ConfigFile = definition.File
				result[definition.Category] = entry
				changed = true
			}
			continue
		}
		directory := strings.TrimSpace(definition.Default())
		if directory == "" {
			continue
		}
		_, file := clientConfigPath(definition, config.ClientConfig{ConfigDir: directory, ConfigFile: definition.File})
		if pathExists(directory) || pathExists(file) {
			entry.ConfigDir = directory
			entry.ConfigFile = definition.File
			result[definition.Category] = entry
			changed = true
		}
	}
	return result, changed
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func publicClientConfigs(cfg config.AppConfig) []PublicClientConfig {
	result := make([]PublicClientConfig, 0, len(clientDefinitions()))
	for _, definition := range clientDefinitions() {
		result = append(result, inspectClientConfig(cfg, definition))
	}
	return result
}

func inspectClientConfig(cfg config.AppConfig, definition clientDefinition) PublicClientConfig {
	entry := cfg.ClientConfigs[definition.Category]
	directory, file := clientConfigPath(definition, entry)
	status := PublicClientConfig{Category: definition.Category, Label: definition.Label, ConfigDir: directory, ConfigFile: file, SkipConfigReplacement: entry.SkipConfigReplacement, RequiresProfile: definition.RequiresProfile, Status: clientStatusNotDetected, StatusText: "未检测到配置"}
	if definition.Kind == "unsupported" {
		status.Status = clientStatusUnsupported
		status.StatusText = "暂不支持自动配置"
		return status
	}
	if !pathExists(directory) && !pathExists(file) {
		return status
	}
	status.Detected = true
	endpoint := clientProxyURL(cfg, definition.Category)
	expectedModel := ""
	if profile := activeProfileForClient(cfg, definition.Category, ""); profile != nil {
		expectedModel = selectedModelID(profile.Models, profile.DefaultModel)
	}
	configured, err := clientConfigurationMatches(definition, directory, file, endpoint, cfg.LocalAccessToken, expectedModel, false)
	if err != nil {
		status.Status = clientStatusError
		status.StatusText = "配置读取失败"
		status.Error = err.Error()
		return status
	}
	if configured {
		status.Status = clientStatusConfigured
		status.StatusText = "已由 CodexRelay 配置"
		status.Configured = true
	} else {
		status.Status = clientStatusNotConfigured
		status.StatusText = "未使用 CodexRelay 配置"
	}
	status.LastChecked = time.Now().Format(time.RFC3339)
	return status
}

func clientProxyURL(source any, category string) string {
	port := config.DefaultProxyPort
	host := "127.0.0.1"
	switch value := source.(type) {
	case config.AppConfig:
		port = value.ProxyPort
		if configuredHost := strings.TrimSpace(value.ClientAccessHost); configuredHost != "" {
			host = configuredHost
		}
	case int:
		// 保留旧的内部调用契约；仅完整 AppConfig 能携带非默认访问主机。
		port = value
	}
	if port <= 0 {
		port = config.DefaultProxyPort
	}
	return fmt.Sprintf("http://%s/%s", net.JoinHostPort(host, strconv.Itoa(port)), category)
}

// ProxyURL 返回当前 config.json 将写入外部客户端的类别地址。
func ProxyURL(cfg config.AppConfig, category string) string { return clientProxyURL(cfg, category) }

// clientConfigurationMatches validates the exact fields consumed by each
// client. It intentionally avoids searching arbitrary bytes, which can yield
// false positives from comments, old providers, or unrelated environment
// values.
func clientConfigurationMatches(definition clientDefinition, directory, file, endpoint, key, expectedModel string, expectNoModel bool) (bool, error) {
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(key) == "" {
		return false, nil
	}
	switch definition.Kind {
	case "claude":
		value, err := readJSONObject(file)
		if err != nil {
			return false, fmt.Errorf("解析 %s: %w", filepath.Base(file), err)
		}
		env, _ := value["env"].(map[string]any)
		matches := stringField(env, "ANTHROPIC_BASE_URL") == endpoint && stringField(env, "ANTHROPIC_AUTH_TOKEN") == key
		if expectedModel != "" {
			for _, name := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL"} {
				matches = matches && stringField(env, name) == expectedModel
			}
		} else if expectNoModel {
			for _, name := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL"} {
				matches = matches && stringField(env, name) == ""
			}
		}
		return matches, nil
	case "opencode":
		value, err := readJSONObject(file)
		if err != nil {
			return false, fmt.Errorf("解析 %s: %w", filepath.Base(file), err)
		}
		providers, _ := value["provider"].(map[string]any)
		provider, _ := providers["codexrelay"].(map[string]any)
		options, _ := provider["options"].(map[string]any)
		matches := stringField(provider, "npm") == "@ai-sdk/openai-compatible" && stringField(options, "baseURL") == endpoint && stringField(options, "apiKey") == key
		if expectedModel != "" {
			matches = matches && stringField(value, "model") == "codexrelay/"+expectedModel
		} else if expectNoModel {
			if model, ok := value["model"].(string); ok && strings.HasPrefix(model, "codexrelay/") {
				matches = false
			}
			if _, hasModels := provider["models"]; hasModels {
				matches = false
			}
		}
		return matches, nil
	case "openclaw":
		value, err := readJSONObject(file)
		if err != nil {
			return false, fmt.Errorf("解析 %s: %w", filepath.Base(file), err)
		}
		models, _ := value["models"].(map[string]any)
		providers, _ := models["providers"].(map[string]any)
		provider, _ := providers["codexrelay"].(map[string]any)
		matches := stringField(provider, "baseUrl") == endpoint && stringField(provider, "apiKey") == key && stringField(provider, "api") == "openai-completions"
		if expectedModel != "" {
			agents, _ := value["agents"].(map[string]any)
			defaults, _ := agents["defaults"].(map[string]any)
			modelConfig, _ := defaults["model"].(map[string]any)
			allowed, _ := defaults["models"].(map[string]any)
			_, allowedModel := allowed["codexrelay/"+expectedModel]
			matches = matches && stringField(modelConfig, "primary") == "codexrelay/"+expectedModel && allowedModel
		} else if expectNoModel {
			if modelConfig, ok := func() (map[string]any, bool) {
				agents, ok := value["agents"].(map[string]any)
				if !ok {
					return nil, false
				}
				defaults, ok := agents["defaults"].(map[string]any)
				if !ok {
					return nil, false
				}
				modelConfig, ok := defaults["model"].(map[string]any)
				return modelConfig, ok
			}(); ok {
				if primary, ok := modelConfig["primary"].(string); ok && strings.HasPrefix(primary, "codexrelay/") {
					matches = false
				}
			}
			if _, hasModels := provider["models"]; hasModels {
				matches = false
			}
			if agents, ok := value["agents"].(map[string]any); ok {
				if defaults, ok := agents["defaults"].(map[string]any); ok {
					if allowed, ok := defaults["models"].(map[string]any); ok {
						for id := range allowed {
							if strings.HasPrefix(id, "codexrelay/") {
								matches = false
								break
							}
						}
					}
				}
			}
		}
		return matches, nil
	case "codex":
		configData, err := os.ReadFile(file)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		authData, err := os.ReadFile(filepath.Join(directory, "auth.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		provider := tomlTopLevelValue(string(configData), "model_provider")
		baseURL := tomlSectionValue(string(configData), "model_providers.codexrelay", "base_url")
		var auth map[string]any
		if len(authData) > 0 {
			if err := json.Unmarshal(authData, &auth); err != nil {
				return false, fmt.Errorf("解析 auth.json: %w", err)
			}
		}
		matches := provider == "codexrelay" && baseURL == endpoint && stringField(auth, "OPENAI_API_KEY") == key
		if expectedModel != "" {
			matches = matches && tomlTopLevelValue(string(configData), "model") == expectedModel
		} else if expectNoModel {
			matches = matches && tomlTopLevelValue(string(configData), "model") == ""
		}
		return matches, nil
	case "gemini":
		data, err := os.ReadFile(filepath.Join(directory, ".env"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		matches := dotenvValue(string(data), "GOOGLE_GEMINI_BASE_URL") == endpoint && dotenvValue(string(data), "GEMINI_API_KEY") == key
		if expectedModel != "" {
			matches = matches && dotenvValue(string(data), "GEMINI_MODEL") == expectedModel
		} else if expectNoModel {
			matches = matches && dotenvValue(string(data), "GEMINI_MODEL") == ""
		}
		settingsPath := filepath.Join(directory, "settings.json")
		if pathExists(settingsPath) {
			settings, settingsErr := readJSONObject(settingsPath)
			if settingsErr != nil {
				return false, fmt.Errorf("解析 settings.json: %w", settingsErr)
			}
			security, _ := settings["security"].(map[string]any)
			auth, _ := security["auth"].(map[string]any)
			matches = matches && stringField(auth, "selectedType") == "gemini-api-key"
		}
		return matches, nil
	case "grok":
		data, err := os.ReadFile(file)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		matches := tomlHasSectionPair(string(data), "base_url", endpoint, "api_key", key)
		if expectedModel != "" {
			matches = matches && tomlSectionValue(string(data), "models", "default") == expectedModel
		}
		return matches, nil
	case "hermes":
		data, err := os.ReadFile(file)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		return hermesHasProvider(string(data), endpoint, key, expectedModel, expectNoModel), nil
	default:
		return false, nil
	}
}

func stringField(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	if text, ok := value[key].(string); ok {
		return text
	}
	return ""
}

func tomlValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "\"") {
		if value, err := strconv.Unquote(raw); err == nil {
			return value
		}
	}
	return strings.Trim(raw, "\"'")
}

func tomlTopLevelValue(raw, key string) string {
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			return ""
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return tomlValue(parts[1])
		}
	}
	return ""
}

func tomlSectionValue(raw, section, key string) string {
	inSection := false
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inSection = strings.TrimSpace(strings.Trim(trimmed, "[]")) == section
			continue
		}
		if !inSection {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return tomlValue(parts[1])
		}
	}
	return ""
}

func tomlHasSectionPair(raw, keyA, valueA, keyB, valueB string) bool {
	foundA, foundB := false, false
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			foundA, foundB = false, false
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case keyA:
			foundA = tomlValue(parts[1]) == valueA
		case keyB:
			foundB = tomlValue(parts[1]) == valueB
		}
		if foundA && foundB {
			return true
		}
	}
	return false
}

func dotenvValue(raw, key string) string {
	value := ""
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			candidate := strings.TrimSpace(parts[1])
			if strings.HasPrefix(candidate, "\"") {
				if decoded, err := strconv.Unquote(candidate); err == nil {
					value = decoded
					continue
				}
			}
			value = strings.Trim(candidate, "\"'")
		}
	}
	return value
}

func hermesHasProvider(raw, endpoint, key, expectedModel string, expectNoModel bool) bool {
	inProvider := false
	foundURL, foundKey, foundModel := false, false, expectedModel == ""
	if expectNoModel {
		foundModel = true
	}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- name:") {
			inProvider = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:")) == "codexrelay"
			foundURL, foundKey, foundModel = false, false, expectedModel == ""
			if expectNoModel {
				foundModel = true
			}
			continue
		}
		if !inProvider {
			continue
		}
		if strings.HasPrefix(trimmed, "base_url:") {
			foundURL = tomlValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "base_url:"))) == endpoint
		}
		if strings.HasPrefix(trimmed, "api_key:") {
			foundKey = tomlValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "api_key:"))) == key
		}
		if strings.HasPrefix(trimmed, "model:") {
			if expectNoModel {
				foundModel = false
			} else {
				foundModel = tomlValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "model:"))) == expectedModel
			}
		}
		if foundURL && foundKey && foundModel {
			return true
		}
	}
	return false
}

// DiscoverConfigPaths 只探测已知默认目录，并保留用户已经保存的自定义目录。
func DiscoverConfigPaths(existing map[string]config.ClientConfig) (map[string]config.ClientConfig, bool) {
	return discoverClientConfigPaths(existing)
}

// PublicConfigs 返回所有客户端的脱敏状态；它只读取本地配置文件。
func PublicConfigs(cfg config.AppConfig) []PublicClientConfig {
	return publicClientConfigs(cfg)
}

// Inspect 返回指定分类的本地配置状态，不会访问上游服务。
func Inspect(cfg config.AppConfig, category string) (PublicClientConfig, error) {
	definition, ok := clientDefinitionFor(category)
	if !ok {
		return PublicClientConfig{}, errors.New("未知 API 类别")
	}
	return inspectClientConfig(cfg, definition), nil
}

// Configure 备份并写入指定外部客户端配置；配置内容来自当前本地快照。
func Configure(cfg config.AppConfig, category, profileID string) error {
	_, err := ConfigureWithResult(cfg, category, profileID)
	return err
}

// ConfigureWithResult is the transactional variant of Configure. Every adapter
// returns a rollback-capable result; callers may restore the exact pre-write
// snapshot while it has not been changed again by another process.
func ConfigureWithResult(cfg config.AppConfig, category, profileID string) (ConfigureResult, error) {
	definition, ok := clientDefinitionFor(category)
	if !ok || definition.Kind == "unsupported" {
		return ConfigureResult{}, errors.New("该 API 类别暂不支持自动配置，请手动配置")
	}
	entry := cfg.ClientConfigs[category]
	if err := config.ValidateClientConfig(category, entry); err != nil {
		return ConfigureResult{}, err
	}
	// An empty ConfigDir means the adapter's known default directory. It is
	// valid after explicit user confirmation; only adapters without a target
	// filename are rejected here.
	directory, file := clientConfigPath(definition, entry)
	if strings.TrimSpace(file) == "" {
		return ConfigureResult{}, errors.New("未找到客户端默认配置目录，请先在高级设置中设置配置目录")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ConfigureResult{}, fmt.Errorf("创建配置目录: %w", err)
	}
	endpoint := clientProxyURL(cfg, category)
	key := strings.TrimSpace(cfg.LocalAccessToken)
	profile := activeProfileForClient(cfg, category, profileID)
	var models []config.ModelEntry
	defaultModel := ""
	if profile != nil {
		models = profile.Models
		defaultModel = profile.DefaultModel
	}
	var result ConfigureResult
	var err error
	switch definition.Kind {
	case "claude":
		result, err = configureSingleResult(file, func(existing []byte) ([]byte, error) {
			return renderClaude(existing, endpoint, key, models, defaultModel)
		})
	case "gemini":
		result, err = configureGeminiResult(directory, filepath.Join(directory, ".env"), endpoint, key, models, defaultModel)
	case "opencode":
		result, err = configureSingleResult(file, func(existing []byte) ([]byte, error) {
			return renderOpenCode(existing, endpoint, key, models, defaultModel)
		})
	case "openclaw":
		result, err = configureSingleResult(file, func(existing []byte) ([]byte, error) {
			return renderOpenClaw(existing, endpoint, key, models, defaultModel)
		})
	case "codex":
		result, err = configureCodexResult(file, filepath.Join(directory, "auth.json"), endpoint, key, defaultModel)
	case "grok":
		result, err = configureSingleResult(file, func(existing []byte) ([]byte, error) {
			return renderGrok(existing, endpoint, key, models, defaultModel)
		})
	case "hermes":
		result, err = configureSingleResult(file, func(existing []byte) ([]byte, error) {
			return renderHermes(existing, endpoint, key, models, defaultModel)
		})
	default:
		return ConfigureResult{}, errors.New("该 API 类别暂不支持自动配置，请手动配置")
	}
	if err != nil {
		return result, err
	}
	configured, inspectErr := clientConfigurationMatches(definition, directory, file, endpoint, key, selectedModelID(models, defaultModel), models != nil)
	if inspectErr == nil && configured {
		return result, nil
	}
	rollbackErr := error(nil)
	if result.Rollback != nil {
		rollbackErr = result.Rollback()
	}
	if inspectErr == nil {
		inspectErr = errors.New("写入后校验未通过")
	}
	if rollbackErr != nil {
		return result, fmt.Errorf("外部客户端配置未生效: %v；回退失败: %w", inspectErr, rollbackErr)
	}
	return result, fmt.Errorf("外部客户端配置未生效，已恢复原配置: %w", inspectErr)
}

func configureSingleResult(path string, render func(existing []byte) ([]byte, error)) (ConfigureResult, error) {
	return applyConfigTransaction([]string{path}, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		data, err := render(snapshots[path].data)
		if err != nil {
			return nil, err
		}
		return []ConfigFileChange{{Path: path, Data: data}}, nil
	})
}

// ConfigFileFor 返回分类的默认配置文件名，用于保存高级设置中的自定义目录。
func ConfigFileFor(category string) (string, bool) {
	definition, ok := clientDefinitionFor(category)
	if !ok {
		return "", false
	}
	return definition.File, true
}

// Supports 表示分类是否有已实现的自动配置适配器。
func Supports(category string) bool {
	definition, ok := clientDefinitionFor(category)
	return ok && definition.Kind != "unsupported"
}

// RequiresProfile 表示适配器没有模型目录时无法生成有效配置。
func RequiresProfile(category string) bool {
	definition, ok := clientDefinitionFor(category)
	return ok && definition.RequiresProfile
}
