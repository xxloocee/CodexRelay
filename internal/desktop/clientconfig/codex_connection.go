package clientconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BurntSushi/toml"

	"codexrelay/internal/config"
)

// Connection settings belong to the selected account, never to a retained
// profile. Do not descend into MCP/project tables: their credentials are unrelated.
var codexConnectionKeys = []string{
	"model_provider", "model_providers", "base_url", "openai_base_url", "chatgpt_base_url",
	"api_key", "api_base", "auth_mode", "requires_openai_auth", "wire_api",
	"env_key", "env_key_instructions", "experimental_bearer_token", "http_headers", "env_http_headers",
	"forced_login_method", "forced_chatgpt_workspace_id", "cli_auth_credentials_store",
}

func readCodexTOML(data []byte) (map[string]any, error) {
	value := map[string]any{}
	if err := toml.Unmarshal(data, &value); err != nil {
		// Parser errors can contain source lines with credentials.
		return nil, errors.New("Codex config.toml 无效，请修正 TOML 语法或重复配置后重试")
	}
	return value, nil
}

func marshalCodexTOML(value map[string]any) ([]byte, error) {
	var buffer bytes.Buffer
	// Encode the selector before all other root keys and tables. Encoding its
	// table first would put subsequent root settings inside that TOML table.
	rest := make(map[string]any, len(value))
	for key, item := range value {
		if key != "model_provider" {
			rest[key] = item
		}
	}
	if provider, exists := value["model_provider"]; exists {
		if err := toml.NewEncoder(&buffer).Encode(map[string]any{"model_provider": provider}); err != nil {
			return nil, err
		}
	}
	err := toml.NewEncoder(&buffer).Encode(rest)
	return buffer.Bytes(), err
}

func clearCodexConnection(value map[string]any, clearModel bool) {
	for _, key := range codexConnectionKeys {
		delete(value, key)
	}
	if clearModel {
		for _, key := range codexModelKeys {
			delete(value, key)
		}
	}
	if profiles, ok := value["profiles"].(map[string]any); ok {
		for _, raw := range profiles {
			if profile, ok := raw.(map[string]any); ok {
				clearCodexConnection(profile, clearModel)
			}
		}
	}
}

func codexRelayProvider(endpoint string) map[string]any {
	return map[string]any{
		"name": "ergouzi.life", "base_url": endpoint,
		"wire_api": "responses", "requires_openai_auth": true,
	}
}

// Only the root and selected profile affect the current request channel.
// Keep unused providers/profiles and all unrelated user settings intact.
func clearCodexActiveConnection(value map[string]any) {
	clear := func(scope map[string]any) {
		for _, key := range codexConnectionKeys {
			if key != "model_providers" {
				delete(scope, key)
			}
		}
	}
	clear(value)
	if profile := codexActiveProfile(value); profile != nil {
		clear(profile)
	}
}

func codexActiveProfile(value map[string]any) map[string]any {
	profiles, _ := value["profiles"].(map[string]any)
	profile, _ := profiles[stringField(value, "profile")].(map[string]any)
	return profile
}

func renderCodexRelayTOML(source []byte, endpoint, model string) ([]byte, error) {
	value, err := readCodexTOML(source)
	if err != nil {
		return nil, err
	}
	clearCodexActiveConnection(value)
	value["model_provider"] = codexRelayModelProviderID
	providers, _ := value["model_providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		value["model_providers"] = providers
	}
	provider, _ := providers[codexRelayModelProviderID].(map[string]any)
	if provider == nil {
		provider = map[string]any{}
	}
	for _, key := range codexConnectionKeys {
		delete(provider, key)
	}
	for key, item := range codexRelayProvider(endpoint) {
		if key == "name" && stringField(provider, "name") != "" {
			continue
		}
		provider[key] = item
	}
	providers[codexRelayModelProviderID] = provider
	// A selected profile can carry its own provider table. Remove only the
	// managed provider override so the root request channel wins.
	if profile := codexActiveProfile(value); profile != nil {
		if overrides, ok := profile["model_providers"].(map[string]any); ok {
			delete(overrides, codexRelayModelProviderID)
		}
	}
	// The key written to auth.json must win over an old keyring login.
	value["cli_auth_credentials_store"] = "file"
	if model != "" {
		value["model"] = model
		if profile := codexActiveProfile(value); profile != nil {
			delete(profile, "model")
		}
	}
	return marshalCodexTOML(value)
}

func codexRelayConfigurationMatches(configData, authData []byte, endpoint, key, model string, _ bool) (bool, error) {
	value, err := readCodexTOML(configData)
	if err != nil {
		return false, err
	}
	// A difference requests a write, never permission to switch. Rendering
	// preserves all fields outside the active request channel.
	canonical, err := renderCodexRelayTOML(configData, endpoint, model)
	if err != nil {
		return false, err
	}
	expected, err := readCodexTOML(canonical)
	if err != nil {
		return false, err
	}
	var auth map[string]any
	if json.Unmarshal(authData, &auth) != nil {
		return false, nil
	}
	matches := reflect.DeepEqual(value, expected) && len(auth) == 1 && stringField(auth, "OPENAI_API_KEY") == key
	if model != "" {
		matches = matches && stringField(value, "model") == model
	}
	return matches, nil
}

func codexSelectedProvider(value map[string]any) string {
	provider := stringField(value, "model_provider")
	if profiles, ok := value["profiles"].(map[string]any); ok {
		if profile, ok := profiles[stringField(value, "profile")].(map[string]any); ok {
			if selected := stringField(profile, "model_provider"); selected != "" {
				provider = selected
			}
		}
	}
	if provider == "" {
		provider = "openai"
	}
	return provider
}

// Normalize the restore candidate, not the immutable official backup. A native
// OAuth account uses Codex's built-in OpenAI provider (including its OAuth URL).
func renderCodexOfficialData(configData, authData []byte) ([]byte, []byte, error) {
	value, err := readCodexTOML(configData)
	if err != nil {
		return nil, nil, err
	}
	original, _ := readCodexTOML(configData)
	provider := codexSelectedProvider(value)
	store := stringField(value, "cli_auth_credentials_store")
	if profiles, ok := value["profiles"].(map[string]any); ok {
		if profile, ok := profiles[stringField(value, "profile")].(map[string]any); ok {
			if override := stringField(profile, "cli_auth_credentials_store"); override != "" {
				store = override
			}
		}
	}
	providers, _ := value["model_providers"].(map[string]any)
	selected := providers[provider]
	var auth map[string]any
	if len(authData) > 0 {
		if json.Unmarshal(authData, &auth) != nil || auth == nil {
			return nil, nil, errors.New("Codex 官方认证备份不是有效 JSON 对象")
		}
		var originalAuth map[string]any
		_ = json.Unmarshal(authData, &originalAuth)
		if stringField(auth, "auth_mode") == "chatgpt" {
			tokens, ok := auth["tokens"].(map[string]any)
			if !ok || stringField(tokens, "account_id") == "" ||
				(stringField(tokens, "access_token") == "" && stringField(tokens, "id_token") == "" && stringField(tokens, "refresh_token") == "") {
				return nil, nil, errors.New("Codex 官方账号备份缺少有效登录信息")
			}
			auth = map[string]any{"auth_mode": "chatgpt", "tokens": tokens}
			if refresh, exists := originalAuth["last_refresh"]; exists {
				auth["last_refresh"] = refresh
			}
			if store == "" || store == "file" {
				provider, selected = "openai", nil
			}
		} else if key := stringField(auth, "OPENAI_API_KEY"); key != "" {
			auth = map[string]any{"OPENAI_API_KEY": key}
		} else if len(auth) > 0 {
			return nil, nil, errors.New("Codex 官方认证备份没有可识别的账号或 API Key")
		}
		if !reflect.DeepEqual(auth, originalAuth) {
			authData, err = marshalJSONObject(auth)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if provider == codexRelayModelProviderID || provider == codexLegacyModelProviderID {
		return nil, nil, errors.New("官方备份仍指向 CodexRelay，不能作为官方配置恢复")
	}
	if provider != "openai" && selected == nil {
		return nil, nil, fmt.Errorf("官方备份缺少所选 Provider %q", provider)
	}
	clearCodexConnection(value, false)
	value["model_provider"] = provider
	if selected != nil {
		value["model_providers"] = map[string]any{provider: selected}
	}
	if store != "" {
		value["cli_auth_credentials_store"] = store
	}
	firstLine := string(bytes.SplitN(configData, []byte("\n"), 2)[0])
	if reflect.DeepEqual(value, original) && tomlAssignmentKey(firstLine) == "model_provider" {
		return configData, authData, nil
	}
	data, err := marshalCodexTOML(value)
	return data, authData, err
}

func normalizeCodexRestoreTargets(targets []restoreTarget) error {
	configIndex, authIndex := -1, -1
	for i := range targets {
		switch filepath.Base(targets[i].file.Path) {
		case "config.toml":
			configIndex = i
		case "auth.json":
			authIndex = i
		}
	}
	if configIndex < 0 || authIndex < 0 {
		return errors.New("Codex 官方备份缺少配置或认证文件记录")
	}
	data, auth, err := renderCodexOfficialData(targets[configIndex].desired, targets[authIndex].desired)
	if err != nil {
		return err
	}
	if targets[configIndex].currentExisted {
		data, err = preserveCodexUserSettings(data, targets[configIndex].current)
		if err != nil {
			return err
		}
	}
	targets[configIndex].desired = data
	targets[configIndex].file.Existed = true
	targets[authIndex].desired = auth
	return nil
}

// These values depend on the upstream model/catalog and must not leak from an
// official account or a previous Relay profile into a different model.
var codexModelKeys = []string{
	"model", "review_model", "model_catalog_json", "model_context_window",
	"model_auto_compact_token_limit", "model_max_output_tokens",
}

func preserveCodexUserSettings(official, current []byte) ([]byte, error) {
	target, err := readCodexTOML(official)
	if err != nil {
		return nil, err
	}
	live, err := readCodexTOML(current)
	if err != nil {
		// The restore transaction saves the unreadable current bytes before
		// writing. A damaged live file must not block a validated backup.
		return official, nil
	}
	// Carry all current user settings, including Windows/sandbox settings and
	// future Codex fields, but never copy Relay connection or model overrides.
	clearCodexConnection(live, true)
	accountKeys := make(map[string]bool)
	for _, key := range append(append([]string{}, codexConnectionKeys...), codexModelKeys...) {
		accountKeys[key] = true
	}
	for key := range target {
		if !accountKeys[key] {
			delete(target, key)
		}
	}
	for key, value := range live {
		target[key] = value
	}
	return marshalCodexTOML(target)
}

func RestoreCodexOfficialConfigWithRollback(files []config.ClientConfigBackup, backupDirectory ...string) (func() error, error) {
	return restoreOfficialConfigWithTransform(files, func(targets []restoreTarget) error {
		if err := normalizeCodexRestoreTargets(targets); err != nil {
			return err
		}
		args := append([]string(nil), backupDirectory...)
		if len(args) > 0 {
			for _, target := range targets {
				if filepath.Base(target.file.Path) == "config.toml" {
					args = append(args[:1], codexBackupPrefix(target.current))
				}
			}
		}
		// Persist the same preimages used by rollback before any target changes.
		// Keep these recovery copies even after success; they are not official
		// snapshots and must not replace the validated original login backup.
		for _, target := range targets {
			if target.currentExisted {
				if _, err := backupClientData(target.file.Path, target.current, args...); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Even an already-official login can contain stale provider definitions.
func NormalizeCodexOfficialWithRollback(cfg config.AppConfig, backupDirectory ...string) (func() error, error) {
	definition, _ := clientDefinitionFor(config.CategoryCodex)
	directory, file := clientConfigPath(definition, cfg.ClientConfigs[config.CategoryCodex])
	authPath := filepath.Join(directory, "auth.json")
	args := append([]string(nil), backupDirectory...)
	if len(args) > 0 {
		args = append(args[:1], "")
	}
	result, err := applyConfigTransactionWithBackupPolicy([]string{file, authPath}, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		if len(args) > 1 {
			args[1] = codexBackupPrefix(snapshots[file].data)
		}
		data, auth, err := renderCodexOfficialData(snapshots[file].data, snapshots[authPath].data)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(data, snapshots[file].data) && bytes.Equal(auth, snapshots[authPath].data) {
			return nil, nil
		}
		return []ConfigFileChange{{Path: file, Data: data}, {Path: authPath, Data: auth}}, nil
	}, nil, args...)
	return result.Rollback, err
}

// Skipping replacement is safe only if the existing files already select the
// unique Relay provider. Never advertise a successful switch to official files.
func RequireCodexRelayConfiguration(cfg config.AppConfig) error {
	definition, _ := clientDefinitionFor(config.CategoryCodex)
	directory, file := clientConfigPath(definition, cfg.ClientConfigs[config.CategoryCodex])
	data, err := os.ReadFile(file)
	if err != nil {
		return errors.New("请先配置 Codex 客户端，不能跳过配置后切换令牌")
	}
	auth, err := os.ReadFile(filepath.Join(directory, "auth.json"))
	if err != nil {
		return errors.New("请先配置 Codex 认证信息，不能跳过配置后切换令牌")
	}
	matches, err := codexRelayConfigurationMatches(data, auth, clientProxyURL(cfg, config.CategoryCodex), cfg.LocalAccessToken, "", false)
	if err != nil {
		return err
	}
	if !matches {
		return errors.New("Codex 的 Provider 或认证配置不一致，请关闭“跳过配置文件替换”并配置后再切换")
	}
	return nil
}
