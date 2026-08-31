/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Hermes YAML 自定义 provider 配置写入
 * @File          : Hermes 客户端适配器
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"errors"
	"strconv"
	"strings"

	"codexrelay/internal/config"
)

func configureHermes(path, endpoint, key string, models []config.ModelEntry, defaultModel string) error {
	_, err := configureSingleResult(path, func(existing []byte) ([]byte, error) {
		return renderHermes(existing, endpoint, key, models, defaultModel)
	})
	return err
}

func renderHermes(existing []byte, endpoint, key string, models []config.ModelEntry, defaultModel string) ([]byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, err
	}
	raw := strings.ReplaceAll(string(existing), "\r\n", "\n")
	blockLines := hermesProviderLines(endpoint, key, models, defaultModel)
	if strings.TrimSpace(raw) == "" {
		raw = strings.Join(append([]string{"custom_providers:"}, blockLines...), "\n") + "\n"
		return []byte(raw), nil
	}
	sectionStart := strings.Index(raw, "custom_providers:")
	if sectionStart < 0 {
		raw = strings.TrimRight(raw, "\n") + "\n\n" + strings.Join(append([]string{"custom_providers:"}, blockLines...), "\n") + "\n"
		return []byte(raw), nil
	}
	// 通过行边界定位下一个顶层键，避免覆盖 custom_providers 以外的 YAML 区域。
	lines := strings.Split(raw, "\n")
	startLine := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "custom_providers:" {
			startLine = i
			break
		}
	}
	if startLine < 0 {
		return nil, errors.New("无法定位 Hermes custom_providers 配置区域")
	}
	endLine := len(lines)
	for i := startLine + 1; i < len(lines); i++ {
		if lines[i] != "" && len(lines[i]) > 0 && lines[i][0] != ' ' && lines[i][0] != '\t' {
			endLine = i
			break
		}
	}
	providerStart := -1
	for i := startLine + 1; i < endLine; i++ {
		if strings.TrimSpace(lines[i]) == "- name: codexrelay" {
			providerStart = i
			break
		}
	}
	if providerStart < 0 {
		insert := blockLines
		lines = append(append(append([]string{}, lines[:startLine+1]...), insert...), lines[startLine+1:]...)
	} else {
		providerEnd := endLine
		for i := providerStart + 1; i < endLine; i++ {
			if strings.HasPrefix(lines[i], "  - ") {
				providerEnd = i
				break
			}
		}
		existing := append([]string(nil), lines[providerStart:providerEnd]...)
		merged := mergeHermesProviderLines(existing, endpoint, key, models, defaultModel)
		lines = append(append(append([]string{}, lines[:providerStart]...), merged...), lines[providerEnd:]...)
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"), nil
}

func hermesProviderLines(endpoint, key string, models []config.ModelEntry, defaultModel string) []string {
	lines := []string{"  - name: codexrelay", "    base_url: " + yamlScalar(endpoint), "    api_key: " + yamlScalar(key), "    api_mode: responses"}
	if len(models) == 0 {
		return lines
	}
	if defaultModel == "" || !containsModel(models, defaultModel) {
		defaultModel = models[0].ID
	}
	lines = append(lines, "    model: "+strconv.Quote(defaultModel), "    models:")
	for _, model := range models {
		lines = append(lines, "      "+strconv.Quote(model.ID)+": {}")
	}
	return lines
}

func mergeHermesProviderLines(existing []string, endpoint, key string, models []config.ModelEntry, defaultModel string) []string {
	if len(existing) == 0 {
		return hermesProviderLines(endpoint, key, models, defaultModel)
	}
	result := append([]string(nil), existing...)
	body := result[1:]
	body = upsertYAMLLine(body, "base_url", "    base_url: "+yamlScalar(endpoint))
	body = upsertYAMLLine(body, "api_key", "    api_key: "+yamlScalar(key))
	body = upsertYAMLLine(body, "api_mode", "    api_mode: responses")
	if models != nil {
		filtered := make([]string, 0, len(body)+len(models)+2)
		for index := 0; index < len(body); index++ {
			line := body[index]
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "model:") {
				continue
			}
			if trimmed == "models:" {
				for index+1 < len(body) && (strings.HasPrefix(body[index+1], "      ") || strings.TrimSpace(body[index+1]) == "") {
					index++
				}
				continue
			}
			filtered = append(filtered, line)
		}
		if len(models) > 0 {
			selected := selectedModelID(models, defaultModel)
			filtered = append(filtered, "    model: "+strconv.Quote(selected), "    models:")
			for _, model := range models {
				filtered = append(filtered, "      "+strconv.Quote(model.ID)+": {}")
			}
		}
		body = filtered
	}
	return append([]string{result[0]}, body...)
}

func yamlScalar(value string) string {
	if value != "" {
		lower := strings.ToLower(value)
		if strings.HasPrefix(value, "-") || strings.HasPrefix(value, ":") || lower == "null" || lower == "true" || lower == "false" || lower == "~" {
			return strconv.Quote(value)
		}
		for _, r := range value {
			if !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return strconv.Quote(value)
			}
		}
		return value
	}
	return strconv.Quote(value)
}
