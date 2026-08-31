/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Gemini 的 .env 与 settings.json 配置写入
 * @File          : Gemini 客户端适配器
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"codexrelay/internal/config"
)

func configureGeminiSettings(path string) error {
	data, err := renderGeminiSettings(path)
	if err != nil || data == nil {
		return err
	}
	_, err = applyConfigChanges([]ConfigFileChange{{Path: path, Data: data}})
	return err
}

func renderGeminiSettings(path string) ([]byte, error) {
	if !pathExists(path) {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 Gemini settings.json 失败: %w", err)
	}
	return renderGeminiSettingsData(data)
}

func renderGeminiSettingsData(data []byte) ([]byte, error) {
	value := map[string]any{}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("解析 Gemini settings.json 失败，请手动配置: %w", err)
	}
	security, ok := value["security"].(map[string]any)
	if !ok {
		security = map[string]any{}
		value["security"] = security
	}
	auth, ok := security["auth"].(map[string]any)
	if !ok {
		auth = map[string]any{}
		security["auth"] = auth
	}
	auth["selectedType"] = "gemini-api-key"
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码 Gemini settings.json 失败: %w", err)
	}
	return append(encoded, '\n'), nil
}

// configureGemini 保持 .env 和 settings.json 的写入顺序与原适配器一致。
func configureGemini(directory, file, endpoint, key string, models []config.ModelEntry, defaultModel string) error {
	_, err := configureGeminiResult(directory, file, endpoint, key, models, defaultModel)
	return err
}

func configureGeminiResult(directory, file, endpoint, key string, models []config.ModelEntry, defaultModel string) (ConfigureResult, error) {
	envPath := file
	if directory != "" {
		envPath = filepath.Join(directory, ".env")
	}
	settingsPath := filepath.Join(directory, "settings.json")
	return applyConfigTransaction([]string{envPath, settingsPath}, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		envData, err := renderDotEnvData(snapshots[envPath].data, endpoint, key, "GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY", models, defaultModel)
		if err != nil {
			return nil, err
		}
		changes := []ConfigFileChange{{Path: envPath, Data: envData}}
		if snapshots[settingsPath].existed {
			settingsData, err := renderGeminiSettingsData(snapshots[settingsPath].data)
			if err != nil {
				return nil, err
			}
			changes = append(changes, ConfigFileChange{Path: settingsPath, Data: settingsData})
		}
		return changes, nil
	})
}
