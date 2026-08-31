/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : .env 配置中的地址、密钥和模型字段更新
 * @File          : 环境变量配置辅助
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"codexrelay/internal/config"
)

func configureDotEnv(path, endpoint, key, baseKey, tokenKey string) error {
	return configureDotEnvWithModel(path, endpoint, key, baseKey, tokenKey, nil, "")
}

func configureDotEnvWithModel(path, endpoint, key, baseKey, tokenKey string, models []config.ModelEntry, defaultModel string) error {
	content, err := renderDotEnvWithModel(path, endpoint, key, baseKey, tokenKey, models, defaultModel)
	if err != nil {
		return err
	}
	_, err = applyConfigChanges([]ConfigFileChange{{Path: path, Data: content}})
	return err
}

func renderDotEnvWithModel(path, endpoint, key, baseKey, tokenKey string, models []config.ModelEntry, defaultModel string) ([]byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), err)
	}
	return renderDotEnvData(data, endpoint, key, baseKey, tokenKey, models, defaultModel)
}

func renderDotEnvData(data []byte, endpoint, key, baseKey, tokenKey string, models []config.ModelEntry, defaultModel string) ([]byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	lines = upsertEnvLine(lines, baseKey, envScalar(endpoint))
	lines = upsertEnvLine(lines, tokenKey, envScalar(key))
	modelKey := "GEMINI_MODEL"
	if baseKey != "GOOGLE_GEMINI_BASE_URL" {
		modelKey = "MODEL"
	}
	if model := selectedModelID(models, defaultModel); model != "" {
		lines = upsertEnvLine(lines, modelKey, envScalar(model))
	} else if models != nil {
		lines = removeEnvLine(lines, modelKey)
	}
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	return []byte(content), nil
}

func envScalar(value string) string {
	if value != "" {
		for _, r := range value {
			if !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '=' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return strconv.Quote(value)
			}
		}
		return value
	}
	return strconv.Quote(value)
}

func upsertEnvLine(lines []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(lines)+1)
	written := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		candidate := strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		parts := strings.SplitN(candidate, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			if !written {
				result = append(result, prefix+value)
				written = true
			}
			continue
		}
		result = append(result, line)
	}
	if !written {
		result = append(result, prefix+value)
	}
	return result
}

func removeEnvLine(lines []string, key string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			continue
		}
		result = append(result, line)
	}
	return result
}
