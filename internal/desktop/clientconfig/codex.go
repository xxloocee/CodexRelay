/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex 配置文件的地址、模型和本地密钥写入
 * @File          : Codex 客户端适配器
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// codexOfficialConfigurationMatches recognizes the native ChatGPT OAuth
// configuration without relying on CodexRelay's persisted mode. External
// switchers such as cc-switch can replace both files while the Relay process
// is not running, so the files themselves are the source of truth here.
func codexOfficialConfigurationMatches(configData, authData []byte) (bool, error) {
	if len(authData) == 0 {
		return false, nil
	}
	var auth map[string]any
	if err := json.Unmarshal(authData, &auth); err != nil {
		return false, fmt.Errorf("解析 auth.json: %w", err)
	}
	if stringField(auth, "auth_mode") != "chatgpt" {
		return false, nil
	}
	tokens, ok := auth["tokens"].(map[string]any)
	if !ok || stringField(tokens, "account_id") == "" {
		return false, nil
	}
	if stringField(tokens, "access_token") == "" && stringField(tokens, "id_token") == "" && stringField(tokens, "refresh_token") == "" {
		return false, nil
	}
	provider := tomlTopLevelValue(string(configData), "model_provider")
	return provider == "" || provider == "openai", nil
}

func configureCodex(configPath, authPath, endpoint, key, defaultModel string) error {
	_, err := configureCodexResult(configPath, authPath, endpoint, key, defaultModel)
	return err
}

func configureCodexResult(configPath, authPath, endpoint, key, defaultModel string, backupDirectory ...string) (ConfigureResult, error) {
	return configureCodexResultWithBackupPolicy(configPath, authPath, endpoint, key, defaultModel, nil, backupDirectory...)
}

func configureCodexResultWithBackupPolicy(configPath, authPath, endpoint, key, defaultModel string, shouldBackup func(string) bool, backupDirectory ...string) (ConfigureResult, error) {
	return applyConfigTransactionWithBackupPolicy([]string{configPath, authPath}, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		configData, authData, err := renderCodexData(configPath, snapshots[configPath].data, snapshots[authPath].data, endpoint, key, defaultModel)
		if err != nil {
			return nil, err
		}
		return []ConfigFileChange{{Path: configPath, Data: configData}, {Path: authPath, Data: authData}}, nil
	}, shouldBackup, backupDirectory...)
}

func renderCodex(configPath, authPath, endpoint, key, defaultModel string) ([]byte, []byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("读取 Codex config.toml 失败: %w", err)
	}
	authData, err := os.ReadFile(authPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("读取 Codex auth.json 失败: %w", err)
	}
	return renderCodexData(configPath, data, authData, endpoint, key, defaultModel)
}

func renderCodexData(configPath string, configSource, authSource []byte, endpoint, key, defaultModel string) ([]byte, []byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, nil, err
	}
	configText := upsertTomlProviderWithModel(string(configSource), "codexrelay", endpoint, defaultModel)
	if defaultModel == "" && configSource != nil {
		configText = strings.Join(removeTomlTopLevelLine(strings.Split(strings.ReplaceAll(configText, "\r\n", "\n"), "\n"), "model"), "\n")
		configText = strings.TrimRight(configText, "\n") + "\n"
	}
	// Codex has two mutually exclusive authentication formats. Relay owns
	// auth.json while active, so do not carry the official OAuth branch into it.
	auth := map[string]any{"OPENAI_API_KEY": key}
	authData, err := marshalJSONObject(auth)
	if err != nil {
		return nil, nil, fmt.Errorf("编码 Codex auth.json 失败: %w", err)
	}
	return []byte(configText), authData, nil
}
