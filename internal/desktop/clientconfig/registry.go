/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 外部 AI 客户端路径探测、接管状态检查与受控写入
 * @File          : 外部客户端配置适配器实现
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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

	ClientConfigStateOfficial               = "official"
	ClientConfigStateManaged                = "managed"
	ClientConfigStateManagedWithoutSnapshot = "managed_without_snapshot"
	ClientConfigStateUnmanaged              = "unmanaged"
)

// PublicClientConfig 是高级设置和启用前检查使用的脱敏状态，不返回外部配置正文。
type PublicClientConfig struct {
	Category                string `json:"category"`
	Label                   string `json:"label"`
	ConfigDir               string `json:"configDir"`
	ConfigFile              string `json:"configFile"`
	SkipConfigReplacement   bool   `json:"skipConfigReplacement"`
	OfficialBackupAvailable bool   `json:"officialBackupAvailable"`
	ConfigState             string `json:"configState"`
	Status                  string `json:"status"`
	Detected                bool   `json:"detected"`
	Configured              bool   `json:"configured"`
	RequiresProfile         bool   `json:"requiresProfile"`
	StatusText              string `json:"statusText"`
	LastChecked             string `json:"lastChecked"`
	Error                   string `json:"error,omitempty"`
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
	filename := definition.File
	if expected, supported := config.ClientConfigFileFor(definition.Category); supported {
		filename = expected
	}
	if directory == "" || filename == "" {
		return directory, ""
	}
	return directory, filepath.Join(directory, filename)
}

// discoverClientConfigPaths 只检查各软件已知默认目录，不遍历磁盘；已有自定义路径永远优先保留。
func discoverClientConfigPaths(cfg config.AppConfig) (map[string]config.ClientConfig, bool) {
	existing := cfg.ClientConfigs
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
		if clientConfigDetected(definition, directory, file) {
			entry.ConfigDir = directory
			entry.ConfigFile = definition.File
			result[definition.Category] = entry
			changed = true
		}
	}
	// v2.3.5 introduced persisted ownership metadata after older releases had
	// already written Relay configuration. Named adapters have durable markers;
	// Claude and Gemini do not, so migrate them only when the complete saved
	// endpoint and token still match.
	for _, definition := range clientDefinitions() {
		entry := result[definition.Category]
		if definition.Kind == "unsupported" || entry.Mode != "" || len(entry.OfficialBackups) > 0 {
			continue
		}
		directory, file := clientConfigPath(definition, entry)
		owned, err := clientConfigurationOwnedByRelay(definition, directory, file)
		if err == nil && owned && relayOwnershipRequiresPersistedMode(definition) {
			owned, err = clientConfigurationMatches(
				definition,
				directory,
				file,
				clientProxyURL(cfg, definition.Category),
				cfg.LocalAccessToken,
				"",
				false,
			)
		}
		if err == nil && owned {
			entry.Mode = "relay"
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
	status := PublicClientConfig{Category: definition.Category, Label: definition.Label, ConfigDir: directory, ConfigFile: file, SkipConfigReplacement: entry.SkipConfigReplacement, OfficialBackupAvailable: len(entry.OfficialBackups) > 0, ConfigState: clientConfigState(entry, false, false), RequiresProfile: definition.RequiresProfile, Status: clientStatusNotDetected, StatusText: "未检测到配置"}
	if definition.Kind == "unsupported" {
		status.Status = clientStatusUnsupported
		status.StatusText = "暂不支持自动配置"
		return status
	}
	snapshots, snapshotErr := readClientConfigSnapshots(definition, directory, file)
	if snapshotErr != nil {
		status.Status = clientStatusError
		status.StatusText = "配置读取失败"
		status.Error = snapshotErr.Error()
		return status
	}
	detected := clientConfigurationDetectedSnapshots(snapshots)
	if !detected {
		status.ConfigState = clientConfigState(entry, false, false)
		switch status.ConfigState {
		case ClientConfigStateOfficial:
			status.Status = clientStatusNotConfigured
			status.StatusText = "使用官方配置"
		case ClientConfigStateManaged:
			status.Status = clientStatusNotConfigured
			status.StatusText = "CodexRelay 配置缺失，需要重建"
		case ClientConfigStateManagedWithoutSnapshot:
			status.Status = clientStatusNotConfigured
			status.StatusText = "CodexRelay 配置缺失，需要重建（无官方快照）"
		}
		return status
	}
	status.Detected = true
	if definition.Kind == "codex" {
		configData := snapshots[file].data
		authData := snapshots[filepath.Join(directory, "auth.json")].data
		official, officialErr := codexOfficialConfigurationMatches(configData, authData)
		if officialErr != nil {
			status.Status = clientStatusError
			status.StatusText = "配置读取失败"
			status.Error = officialErr.Error()
			return status
		}
		if official {
			status.ConfigState = ClientConfigStateOfficial
			status.Status = clientStatusNotConfigured
			status.StatusText = "使用官方配置"
			status.LastChecked = time.Now().Format(time.RFC3339)
			return status
		}
	}
	relayOwned, ownershipErr := clientConfigurationOwnedByRelaySnapshots(definition, directory, file, snapshots)
	if ownershipErr != nil {
		status.Status = clientStatusError
		status.StatusText = "配置读取失败"
		status.Error = ownershipErr.Error()
		return status
	}
	partiallyRelayOwned, ownershipErr := clientConfigurationPartiallyOwnedByRelaySnapshots(definition, entry, directory, file, snapshots)
	if ownershipErr != nil {
		status.Status = clientStatusError
		status.StatusText = "配置读取失败"
		status.Error = ownershipErr.Error()
		return status
	}
	owned := false
	if len(entry.OfficialBackups) > 0 {
		owned = clientSnapshotsMatchExpectedOrMissing(snapshots, entry.OfficialBackups) &&
			(detected || entry.Mode == "relay")
	} else {
		owned = relayOwned
		if relayOwnershipRequiresPersistedMode(definition) && entry.Mode != "relay" {
			owned = false
		}
	}
	if !owned && (relayOwned || partiallyRelayOwned) {
		status.ConfigState = ClientConfigStateUnmanaged
	} else {
		status.ConfigState = clientConfigState(entry, true, owned)
	}
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
	if configured && IsManagedState(status.ConfigState) {
		status.Status = clientStatusConfigured
		status.StatusText = "已由 CodexRelay 配置"
		status.Configured = true
	} else {
		status.Status = clientStatusNotConfigured
		switch status.ConfigState {
		case ClientConfigStateOfficial:
			status.StatusText = "使用官方配置"
		case ClientConfigStateManaged:
			status.StatusText = "CodexRelay 配置需要更新"
		case ClientConfigStateManagedWithoutSnapshot:
			status.StatusText = "CodexRelay 配置需要更新（无官方快照）"
		default:
			status.StatusText = "未使用 CodexRelay 配置"
		}
	}
	status.LastChecked = time.Now().Format(time.RFC3339)
	return status
}

func clientConfigState(entry config.ClientConfig, detected, owned bool) string {
	if owned {
		if len(entry.OfficialBackups) == 0 {
			return ClientConfigStateManagedWithoutSnapshot
		}
		return ClientConfigStateManaged
	}
	// Persisted Relay mode is only a recovery hint when every managed file was
	// removed. Any readable but unrecognized content belongs to the client/user
	// and must win over stale ownership metadata.
	if !detected && entry.Mode == "relay" {
		if len(entry.OfficialBackups) == 0 {
			return ClientConfigStateManagedWithoutSnapshot
		}
		return ClientConfigStateManaged
	}
	if entry.Mode == "official" {
		return ClientConfigStateOfficial
	}
	return ClientConfigStateUnmanaged
}

func IsManagedState(state string) bool {
	return state == ClientConfigStateManaged || state == ClientConfigStateManagedWithoutSnapshot
}

func relayOwnershipRequiresPersistedMode(definition clientDefinition) bool {
	return definition.Kind == "claude" || definition.Kind == "gemini"
}

func relayOwnershipConfirmed(definition clientDefinition, entry config.ClientConfig, directory, file string, snapshots map[string]configFileSnapshot) (bool, error) {
	owned, err := clientConfigurationOwnedByRelaySnapshots(definition, directory, file, snapshots)
	if err != nil || !owned {
		return owned, err
	}
	if relayOwnershipRequiresPersistedMode(definition) && entry.Mode != "relay" {
		return false, nil
	}
	return true, nil
}

func clientConfigurationPartiallyOwnedByRelaySnapshots(definition clientDefinition, entry config.ClientConfig, directory, file string, snapshots map[string]configFileSnapshot) (bool, error) {
	owned, err := clientConfigurationOwnedByRelaySnapshots(definition, directory, file, snapshots)
	if err != nil || owned {
		return false, err
	}
	switch definition.Kind {
	case "codex":
		data := string(snapshots[file].data)
		provider := strings.ToLower(strings.TrimSpace(tomlTopLevelValue(data, "model_provider")))
		configOwned := false
		if provider == codexRelayModelProviderID || provider == codexLegacyModelProviderID {
			section := "model_providers." + provider
			configOwned = tomlSectionValue(data, section, "wire_api") == "responses" &&
				tomlSectionValue(data, section, "requires_openai_auth") == "true" &&
				relayRouteURL(tomlSectionValue(data, section, "base_url"), config.CategoryCodex)
		}
		var auth map[string]any
		authData := snapshots[filepath.Join(directory, "auth.json")].data
		authOwned := len(authData) > 0 && json.Unmarshal(authData, &auth) == nil &&
			len(auth) == 1 && strings.TrimSpace(stringField(auth, "OPENAI_API_KEY")) != ""
		return configOwned || authOwned, nil
	case "gemini":
		if len(entry.OfficialBackups) == 0 && entry.Mode != "relay" {
			return false, nil
		}
		data := string(snapshots[file].data)
		envOwned := relayRouteURL(dotenvValue(data, "GOOGLE_GEMINI_BASE_URL"), config.CategoryGemini) &&
			strings.TrimSpace(dotenvValue(data, "GEMINI_API_KEY")) != ""
		settingsOwned := false
		settings := snapshots[filepath.Join(directory, "settings.json")]
		if settings.existed {
			value, err := readJSONObjectData(settings.data)
			if err != nil {
				return false, err
			}
			security, _ := value["security"].(map[string]any)
			auth, _ := security["auth"].(map[string]any)
			settingsOwned = stringField(auth, "selectedType") == "gemini-api-key"
		}
		return envOwned || settingsOwned, nil
	default:
		return false, nil
	}
}

// clientConfigurationOwnedByRelay recognizes adapter-owned structure without
// coupling ownership to the current port or local token. Only explicit markers
// emitted by a Relay renderer are accepted, so unknown client content can still
// become a fresh official baseline after user confirmation.
func clientConfigurationOwnedByRelay(definition clientDefinition, directory, file string) (bool, error) {
	snapshots, err := readClientConfigSnapshots(definition, directory, file)
	if err != nil {
		return false, err
	}
	return clientConfigurationOwnedByRelaySnapshots(definition, directory, file, snapshots)
}

func readClientConfigSnapshots(definition clientDefinition, directory, file string) (map[string]configFileSnapshot, error) {
	paths := clientConfigTargetPaths(definition, directory, file)
	result := make(map[string]configFileSnapshot, len(paths))
	for _, path := range paths {
		snapshot := configFileSnapshot{path: path, mode: 0o600}
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			result[path] = snapshot
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("配置目标 %s 是目录", filepath.Base(path))
		}
		snapshot.existed = true
		snapshot.mode = info.Mode().Perm()
		snapshot.data, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		result[path] = snapshot
	}
	return result, nil
}

func clientConfigTargetPaths(definition clientDefinition, directory, file string) []string {
	paths := []string{file}
	switch definition.Kind {
	case "codex":
		paths = append(paths, filepath.Join(directory, "auth.json"))
	case "gemini":
		paths = append(paths, filepath.Join(directory, "settings.json"))
	}
	return paths
}

func clientConfigurationOwnedByRelaySnapshots(definition clientDefinition, directory, file string, snapshots map[string]configFileSnapshot) (bool, error) {
	data := snapshots[file].data
	switch definition.Kind {
	case "codex":
		provider := strings.ToLower(strings.TrimSpace(tomlTopLevelValue(string(data), "model_provider")))
		if provider != codexRelayModelProviderID && provider != codexLegacyModelProviderID {
			return false, nil
		}
		section := "model_providers." + provider
		if tomlSectionValue(string(data), section, "wire_api") != "responses" ||
			tomlSectionValue(string(data), section, "requires_openai_auth") != "true" ||
			!relayRouteURL(tomlSectionValue(string(data), section, "base_url"), config.CategoryCodex) {
			return false, nil
		}
		var auth map[string]any
		authData := snapshots[filepath.Join(directory, "auth.json")].data
		if len(authData) == 0 || json.Unmarshal(authData, &auth) != nil {
			return false, nil
		}
		return len(auth) == 1 && strings.TrimSpace(stringField(auth, "OPENAI_API_KEY")) != "", nil
	case "claude":
		value, err := readJSONObjectData(data)
		if err != nil {
			return false, err
		}
		env, _ := value["env"].(map[string]any)
		return relayRouteURL(stringField(env, "ANTHROPIC_BASE_URL"), config.CategoryClaude) && strings.TrimSpace(stringField(env, "ANTHROPIC_AUTH_TOKEN")) != "", nil
	case "gemini":
		owned := relayRouteURL(dotenvValue(string(data), "GOOGLE_GEMINI_BASE_URL"), config.CategoryGemini) && strings.TrimSpace(dotenvValue(string(data), "GEMINI_API_KEY")) != ""
		if !owned {
			return false, nil
		}
		settings := snapshots[filepath.Join(directory, "settings.json")]
		if !settings.existed {
			return true, nil
		}
		value, err := readJSONObjectData(settings.data)
		if err != nil {
			return false, err
		}
		security, _ := value["security"].(map[string]any)
		auth, _ := security["auth"].(map[string]any)
		return stringField(auth, "selectedType") == "gemini-api-key", nil
	case "opencode":
		value, err := readJSONObjectData(data)
		if err != nil {
			return false, err
		}
		providers, _ := value["provider"].(map[string]any)
		provider, ok := providers["codexrelay"].(map[string]any)
		return ok && stringField(provider, "npm") == "@ai-sdk/openai-compatible", nil
	case "openclaw":
		value, err := readJSONObjectData(data)
		if err != nil {
			return false, err
		}
		models, _ := value["models"].(map[string]any)
		providers, _ := models["providers"].(map[string]any)
		provider, ok := providers["codexrelay"].(map[string]any)
		return ok && stringField(provider, "api") == "openai-completions", nil
	case "grok":
		return strings.Contains(string(data), "# CodexRelay managed model"), nil
	case "hermes":
		for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) == "- name: codexrelay" {
				return true, nil
			}
		}
	}
	return false, nil
}

func relayRouteURL(raw, category string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Hostname() == "" || parsed.Port() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.EscapedPath() == "/"+category
}

func clientConfigDetected(definition clientDefinition, directory, file string) bool {
	paths := []string{file}
	switch definition.Kind {
	case "codex":
		paths = append(paths, filepath.Join(directory, "auth.json"))
	case "gemini":
		paths = append(paths, filepath.Join(directory, "settings.json"))
	}
	for _, path := range paths {
		if pathExists(path) {
			return true
		}
	}
	return false
}

func clientProxyURL(cfg config.AppConfig, category string) string {
	port := config.DefaultProxyPort
	host := "127.0.0.1"
	port = cfg.ProxyPort
	if configuredHost := strings.TrimSpace(cfg.ClientAccessHost); configuredHost != "" {
		host = configuredHost
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
		provider := strings.ToLower(strings.TrimSpace(tomlTopLevelValue(string(configData), "model_provider")))
		providerSection := ""
		if provider == codexRelayModelProviderID || provider == codexLegacyModelProviderID {
			providerSection = "model_providers." + provider
		}
		baseURL := tomlSectionValue(string(configData), providerSection, "base_url")
		var auth map[string]any
		if len(authData) > 0 {
			if err := json.Unmarshal(authData, &auth); err != nil {
				return false, fmt.Errorf("解析 auth.json: %w", err)
			}
		}
		// Codex's Responses API provider and local API-key auth are both Relay
		// ownership markers. Requiring the exact auth object prevents an OAuth
		// config or unrelated credentials from being treated as managed.
		wireAPI := tomlSectionValue(string(configData), providerSection, "wire_api")
		requiresOpenAIAuth := tomlSectionValue(string(configData), providerSection, "requires_openai_auth")
		matches := providerSection != "" && baseURL == endpoint && wireAPI == "responses" && requiresOpenAIAuth == "true" && len(auth) == 1 && stringField(auth, "OPENAI_API_KEY") == key
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
func DiscoverConfigPaths(cfg config.AppConfig) (map[string]config.ClientConfig, bool) {
	return discoverClientConfigPaths(cfg)
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

// ValidateClientConfigTargets rejects configurations where two adapters would
// write the same physical file. Their official snapshots are category-scoped,
// so allowing an overlap would make one adapter's restore erase another's.
func ValidateClientConfigTargets(clients map[string]config.ClientConfig) error {
	type targetOwner struct {
		label string
	}
	owners := make(map[string]targetOwner)
	for _, definition := range clientDefinitions() {
		if definition.Kind == "unsupported" {
			continue
		}
		entry := clients[definition.Category]
		directory, file := clientConfigPath(definition, entry)
		if strings.TrimSpace(file) == "" {
			continue
		}
		paths := []string{file}
		if definition.Kind == "codex" {
			paths = append(paths, filepath.Join(directory, "auth.json"))
		}
		if definition.Kind == "gemini" {
			paths = append(paths, filepath.Join(directory, "settings.json"))
		}
		for _, path := range paths {
			cleaned := filepath.Clean(path)
			key := cleaned
			if runtime.GOOS == "windows" {
				key = strings.ToLower(cleaned)
			}
			if previous, exists := owners[key]; exists {
				return fmt.Errorf("%s 与 %s 不能使用同一个配置文件 %s", definition.Label, previous.label, cleaned)
			}
			owners[key] = targetOwner{label: definition.Label}
		}
	}
	return nil
}

// ValidateExternalClientDirectory prevents a client target from being placed
// inside Relay's own data tree, where generated files and official snapshots
// would otherwise share the same namespace.
func ValidateExternalClientDirectory(dataDirectory, clientDirectory string) error {
	dataDirectory = filepath.Clean(strings.TrimSpace(dataDirectory))
	clientDirectory = filepath.Clean(strings.TrimSpace(clientDirectory))
	if dataDirectory == "" || clientDirectory == "" || !filepath.IsAbs(dataDirectory) || !filepath.IsAbs(clientDirectory) {
		return errors.New("客户端配置目录必须是绝对路径")
	}
	root := dataDirectory
	target := clientDirectory
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		target = strings.ToLower(target)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("检查客户端配置目录失败: %w", err)
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return errors.New("客户端配置目录不能位于 CodexRelay 数据目录内")
	}
	return nil
}

// ValidateOfficialConfigFiles ensures persisted restore metadata can only
// address files owned by the selected client adapter. The metadata lives in a
// user-editable JSON file, so restore paths must not be trusted blindly.
func ValidateOfficialConfigFiles(cfg config.AppConfig, category string, files []config.ClientConfigBackup) error {
	definition, ok := clientDefinitionFor(category)
	if !ok || definition.Kind == "unsupported" {
		return errors.New("该 API 类别暂不支持官方配置恢复")
	}
	entry := cfg.ClientConfigs[category]
	directory, file := clientConfigPath(definition, entry)
	allowed := map[string]struct{}{filepath.Clean(file): struct{}{}}
	if definition.Kind == "codex" {
		allowed[filepath.Join(directory, "auth.json")] = struct{}{}
	}
	if definition.Kind == "gemini" {
		allowed[filepath.Join(directory, ".env")] = struct{}{}
		allowed[filepath.Join(directory, "settings.json")] = struct{}{}
	}
	seen := make(map[string]struct{}, len(files))
	for _, backup := range files {
		path := filepath.Clean(strings.TrimSpace(backup.Path))
		if path == "." || !filepath.IsAbs(path) {
			return errors.New("官方配置恢复路径无效")
		}
		if _, ok := allowed[path]; !ok {
			return fmt.Errorf("官方配置恢复路径不属于 %s 客户端", definition.Label)
		}
		if _, ok := seen[path]; ok {
			return errors.New("官方配置恢复路径重复")
		}
		seen[path] = struct{}{}
		backupPath := filepath.Clean(strings.TrimSpace(backup.BackupPath))
		if backup.Existed {
			if filepath.IsAbs(backupPath) || filepath.Dir(backupPath) != filepath.Join("client-backups", category) || !strings.HasSuffix(backupPath, ".CodexRelay") || backup.BackupSHA256 == "" {
				return errors.New("官方配置备份路径无效")
			}
		} else if strings.TrimSpace(backup.BackupPath) != "" || strings.TrimSpace(backup.BackupSHA256) != "" {
			return errors.New("不存在的官方配置文件不能包含备份数据")
		}
		if strings.TrimSpace(backup.ExpectedSHA256) == "" {
			return errors.New("官方配置恢复指纹缺失")
		}
	}
	return nil
}

// ResolveOfficialConfigFiles validates relative metadata before resolving it
// against the current Relay data directory. Originals remain client-owned.
func ResolveOfficialConfigFiles(cfg config.AppConfig, category, dataDirectory string) ([]config.ClientConfigBackup, error) {
	files := cfg.ClientConfigs[category].OfficialBackups
	if err := ValidateOfficialConfigFiles(cfg, category, files); err != nil {
		return nil, err
	}
	resolved := append([]config.ClientConfigBackup(nil), files...)
	for index := range resolved {
		if resolved[index].Existed {
			resolved[index].BackupPath = filepath.Join(dataDirectory, resolved[index].BackupPath)
		}
	}
	return resolved, nil
}

type configureIntent uint8

const (
	configureTakeover configureIntent = iota
	configureManagedUpdate
)

// ConfigureWithResult explicitly takes over a client after user confirmation.
// Unknown current content becomes the new official baseline.
func ConfigureWithResult(cfg config.AppConfig, category, profileID, dataDirectory string) (ConfigureResult, error) {
	return configureWithResult(cfg, category, profileID, dataDirectory, configureTakeover)
}

// UpdateManagedWithResult updates an existing Relay-owned client. It never
// takes over unknown content and validates ownership from the transaction's
// immutable pre-write snapshots.
func UpdateManagedWithResult(cfg config.AppConfig, category, profileID, dataDirectory string) (ConfigureResult, error) {
	return configureWithResult(cfg, category, profileID, dataDirectory, configureManagedUpdate)
}

func configureWithResult(cfg config.AppConfig, category, profileID, dataDirectory string, intent configureIntent) (ConfigureResult, error) {
	definition, ok := clientDefinitionFor(category)
	if !ok || definition.Kind == "unsupported" {
		return ConfigureResult{}, errors.New("该 API 类别暂不支持自动配置，请手动配置")
	}
	if err := ValidateClientConfigTargets(cfg.ClientConfigs); err != nil {
		return ConfigureResult{}, err
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
	if strings.TrimSpace(dataDirectory) == "" || !filepath.IsAbs(dataDirectory) {
		return ConfigureResult{}, errors.New("CodexRelay 数据目录必须是绝对路径")
	}
	if err := ValidateExternalClientDirectory(dataDirectory, directory); err != nil {
		return ConfigureResult{}, err
	}
	profile := activeProfileForClient(cfg, category, profileID)
	if profile == nil {
		return ConfigureResult{}, errors.New("请先为该类别启用一个代理 API，再配置客户端")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ConfigureResult{}, fmt.Errorf("创建配置目录: %w", err)
	}
	endpoint := clientProxyURL(cfg, category)
	key := strings.TrimSpace(cfg.LocalAccessToken)
	backupDirectory := ClientBackupDirectory(dataDirectory, category)
	knownOfficialFiles := make(map[string]bool, len(cfg.ClientConfigs[category].OfficialBackups))
	persistedOfficialData := make(map[string][]byte, len(cfg.ClientConfigs[category].OfficialBackups))
	managedWithoutSnapshot := false
	resetOfficialSnapshot := false
	legacyCodexProviderID := ""
	usePersistedOfficialSnapshot := false
	validateSnapshots := func(snapshots map[string]configFileSnapshot) error {
		detected := clientConfigurationDetectedSnapshots(snapshots)
		var relayOwned bool
		var err error
		if len(entry.OfficialBackups) > 0 {
			relayOwned, err = clientConfigurationOwnedByRelaySnapshots(definition, directory, file, snapshots)
		} else {
			relayOwned, err = relayOwnershipConfirmed(definition, entry, directory, file, snapshots)
		}
		if err != nil {
			return fmt.Errorf("检查现有客户端配置归属失败: %w", err)
		}
		partiallyRelayOwned, err := clientConfigurationPartiallyOwnedByRelaySnapshots(definition, entry, directory, file, snapshots)
		if err != nil {
			return fmt.Errorf("检查现有客户端配置归属失败: %w", err)
		}
		codexCurrentlyOfficial := false
		if definition.Kind == "codex" {
			configData := snapshots[file].data
			authData := snapshots[filepath.Join(directory, "auth.json")].data
			legacyCodexProviderID = tomlTopLevelValue(string(configData), "model_provider")
			codexCurrentlyOfficial, err = codexOfficialConfigurationMatches(configData, authData)
			if err != nil {
				return err
			}
		}

		if intent == configureManagedUpdate {
			if len(entry.OfficialBackups) == 0 {
				managedWithoutSnapshot = relayOwned || (!detected && entry.Mode == "relay")
				if !managedWithoutSnapshot {
					return errors.New("当前客户端配置已不再由 CodexRelay 接管；已拒绝自动覆盖，请在主界面重新确认配置")
				}
				return nil
			}
			if !clientSnapshotsMatchExpectedOrMissing(snapshots, entry.OfficialBackups) || (!detected && entry.Mode != "relay") {
				return errors.New("当前客户端配置已被其他工具修改；已拒绝自动覆盖，请在主界面重新确认配置")
			}
			usePersistedOfficialSnapshot = true
		} else {
			if partiallyRelayOwned && len(entry.OfficialBackups) == 0 {
				return errors.New("检测到客户端配置仅部分由 CodexRelay 接管；已拒绝将混合配置保存为官方快照，请先在客户端恢复完整官方配置")
			}
			if len(entry.OfficialBackups) == 0 {
				managedWithoutSnapshot = !codexCurrentlyOfficial &&
					(relayOwned || (!detected && entry.Mode == "relay"))
			} else {
				usePersistedOfficialSnapshot = clientSnapshotsMatchExpectedOrMissing(snapshots, entry.OfficialBackups) ||
					relayOwned || partiallyRelayOwned ||
					(!detected && entry.Mode == "relay")
				resetOfficialSnapshot = !usePersistedOfficialSnapshot
			}
		}

		if usePersistedOfficialSnapshot {
			if err := ValidateOfficialConfigFiles(cfg, category, entry.OfficialBackups); err != nil {
				return fmt.Errorf("官方配置备份信息无效: %w", err)
			}
			for _, backup := range entry.OfficialBackups {
				data, err := readStoredOfficialBackup(cfg, category, backup, backupDirectory)
				if err != nil {
					return err
				}
				path := filepath.Clean(backup.Path)
				knownOfficialFiles[path] = backup.Existed
				if backup.Existed {
					persistedOfficialData[path] = data
				}
			}
		}
		return nil
	}
	renderSource := func(snapshots map[string]configFileSnapshot, path string) []byte {
		snapshot := snapshots[path]
		if snapshot.existed {
			return snapshot.data
		}
		return persistedOfficialData[filepath.Clean(path)]
	}
	shouldBackup := func(path string) bool {
		if managedWithoutSnapshot {
			return false
		}
		_, known := knownOfficialFiles[filepath.Clean(path)]
		// The first transaction fixes the original file set. A file that was
		// absent then remains Relay-created and must not produce an unreferenced
		// backup on every later write.
		return !known
	}
	var models []config.ModelEntry
	defaultModel := ""
	if profile != nil {
		models = profile.Models
		defaultModel = profile.DefaultModel
	}
	paths := clientConfigTargetPaths(definition, directory, file)
	result, err := applyConfigTransactionWithSnapshotPolicy(paths, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		switch definition.Kind {
		case "claude":
			data, err := renderClaude(renderSource(snapshots, file), endpoint, key, models, defaultModel)
			return []ConfigFileChange{{Path: file, Data: data}}, err
		case "gemini":
			envData, err := renderDotEnvData(renderSource(snapshots, file), endpoint, key, "GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY", models, defaultModel)
			if err != nil {
				return nil, err
			}
			changes := []ConfigFileChange{{Path: file, Data: envData}}
			settingsPath := filepath.Join(directory, "settings.json")
			settingsSource := renderSource(snapshots, settingsPath)
			if snapshots[settingsPath].existed || settingsSource != nil {
				settingsData, err := renderGeminiSettingsData(settingsSource)
				if err != nil {
					return nil, err
				}
				changes = append(changes, ConfigFileChange{Path: settingsPath, Data: settingsData})
			}
			return changes, nil
		case "opencode":
			data, err := renderOpenCode(renderSource(snapshots, file), endpoint, key, models, defaultModel)
			return []ConfigFileChange{{Path: file, Data: data}}, err
		case "openclaw":
			data, err := renderOpenClaw(renderSource(snapshots, file), endpoint, key, models, defaultModel)
			return []ConfigFileChange{{Path: file, Data: data}}, err
		case "codex":
			authPath := filepath.Join(directory, "auth.json")
			configData, authData, err := renderCodexData(file, renderSource(snapshots, file), renderSource(snapshots, authPath), endpoint, key, defaultModel)
			return []ConfigFileChange{{Path: file, Data: configData}, {Path: authPath, Data: authData}}, err
		case "grok":
			data, err := renderGrok(renderSource(snapshots, file), endpoint, key, models, defaultModel)
			return []ConfigFileChange{{Path: file, Data: data}}, err
		case "hermes":
			data, err := renderHermes(renderSource(snapshots, file), endpoint, key, models, defaultModel)
			return []ConfigFileChange{{Path: file, Data: data}}, err
		default:
			return nil, errors.New("该 API 类别暂不支持自动配置，请手动配置")
		}
	}, validateSnapshots, shouldBackup, backupDirectory)
	if err != nil {
		return result, err
	}
	result.OfficialSnapshot = !managedWithoutSnapshot
	result.ResetOfficialSnapshot = resetOfficialSnapshot
	configured, inspectErr := clientConfigurationMatches(definition, directory, file, endpoint, key, selectedModelID(models, defaultModel), models != nil)
	if inspectErr == nil && configured {
		if definition.Kind == "codex" {
			historyRollback, migrationErr := migrateCodexHistoryProviderBucket(directory, dataDirectory, legacyCodexProviderID)
			if migrationErr != nil {
				configRollbackErr := error(nil)
				if result.Rollback != nil {
					configRollbackErr = result.Rollback()
				}
				if configRollbackErr != nil {
					return result, fmt.Errorf("迁移 Codex 历史会话失败，请先关闭 Codex 后重试: %v；配置回退失败: %w", migrationErr, configRollbackErr)
				}
				return result, fmt.Errorf("迁移 Codex 历史会话失败，请先关闭 Codex 后重试: %w", migrationErr)
			}
			if historyRollback != nil {
				configRollback := result.Rollback
				result.Rollback = func() error {
					if configRollback == nil {
						return historyRollback()
					}
					return errors.Join(historyRollback(), configRollback())
				}
			}
		}
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

func clientConfigurationDetectedSnapshots(snapshots map[string]configFileSnapshot) bool {
	for _, snapshot := range snapshots {
		if snapshot.existed {
			return true
		}
	}
	return false
}

func clientSnapshotsMatchExpectedOrMissing(snapshots map[string]configFileSnapshot, backups []config.ClientConfigBackup) bool {
	if len(backups) == 0 {
		return false
	}
	byPath := make(map[string]config.ClientConfigBackup, len(backups))
	for _, backup := range backups {
		byPath[filepath.Clean(backup.Path)] = backup
	}
	for path, snapshot := range snapshots {
		if !snapshot.existed {
			continue
		}
		backup, ok := byPath[filepath.Clean(path)]
		if !ok || strings.TrimSpace(backup.ExpectedSHA256) == "" || sha256Hex(snapshot.data) != strings.TrimSpace(backup.ExpectedSHA256) {
			return false
		}
	}
	for _, backup := range backups {
		if _, ok := snapshots[filepath.Clean(backup.Path)]; !ok {
			return false
		}
	}
	return true
}

func readStoredOfficialBackup(cfg config.AppConfig, category string, backup config.ClientConfigBackup, categoryBackupDirectory string) ([]byte, error) {
	if err := ValidateOfficialConfigFiles(cfg, category, []config.ClientConfigBackup{backup}); err != nil {
		return nil, fmt.Errorf("官方配置备份信息无效: %w", err)
	}
	if !backup.Existed {
		return nil, nil
	}
	dataDirectory := filepath.Dir(filepath.Dir(categoryBackupDirectory))
	backupPath := filepath.Join(dataDirectory, backup.BackupPath)
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 官方备份失败: %w", filepath.Base(backup.Path), err)
	}
	if backup.BackupSHA256 == "" || sha256Hex(data) != backup.BackupSHA256 {
		return nil, fmt.Errorf("%s 官方备份校验失败，请先恢复或清理该客户端的官方快照", filepath.Base(backup.Path))
	}
	return data, nil
}

func configureSingleResult(path string, render func(existing []byte) ([]byte, error), backupDirectory ...string) (ConfigureResult, error) {
	return configureSingleResultWithBackupPolicy(path, render, nil, backupDirectory...)
}

func configureSingleResultWithBackupPolicy(path string, render func(existing []byte) ([]byte, error), shouldBackup func(string) bool, backupDirectory ...string) (ConfigureResult, error) {
	return applyConfigTransactionWithBackupPolicy([]string{path}, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		data, err := render(snapshots[path].data)
		if err != nil {
			return nil, err
		}
		return []ConfigFileChange{{Path: path, Data: data}}, nil
	}, shouldBackup, backupDirectory...)
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
