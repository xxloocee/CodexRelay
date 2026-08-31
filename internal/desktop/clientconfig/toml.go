/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 客户端 TOML 配置的片段级更新辅助
 * @File          : TOML 配置辅助
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"strconv"
	"strings"
)

func upsertTomlProvider(raw, providerID, endpoint string) string {
	return upsertTomlProviderWithModel(raw, providerID, endpoint, "")
}

func upsertTomlProviderWithModel(raw, providerID, endpoint, defaultModel string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	foundTop := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}
		if tomlAssignmentKey(line) == "model_provider" {
			lines[i] = "model_provider = " + strconv.Quote(providerID)
			foundTop = true
			break
		}
	}
	if !foundTop {
		lines = append([]string{"model_provider = " + strconv.Quote(providerID)}, lines...)
	}
	if strings.TrimSpace(defaultModel) != "" {
		lines = upsertTomlTopLevelLine(lines, "model", strconv.Quote(defaultModel))
	}
	header := "[model_providers." + providerID + "]"
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			start = i
			break
		}
	}
	if start < 0 {
		block := []string{"", header, "name = \"CodexRelay\"", "base_url = " + strconv.Quote(endpoint), "wire_api = \"responses\"", "requires_openai_auth = true"}
		lines = append(lines, block...)
	} else {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
				end = i
				break
			}
		}
		section := lines[start+1 : end]
		section = upsertTomlLine(section, "name", "\"CodexRelay\"")
		section = upsertTomlLine(section, "base_url", strconv.Quote(endpoint))
		section = upsertTomlLine(section, "wire_api", "\"responses\"")
		section = upsertTomlLine(section, "requires_openai_auth", "true")
		lines = append(append(append([]string{}, lines[:start+1]...), section...), lines[end:]...)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func upsertTomlTopLevelLine(lines []string, key, value string) []string {
	firstSection := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			firstSection = i
			break
		}
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			trimmed := strings.TrimSpace(line)
			if tomlAssignmentKey(trimmed) == key {
				lines[i] = key + " = " + value
				return lines
			}
		}
	}
	lines = append(lines, "")
	copy(lines[firstSection+1:], lines[firstSection:])
	lines[firstSection] = key + " = " + value
	return lines
}

func removeTomlTopLevelLine(lines []string, key string) []string {
	firstSection := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			firstSection = i
			break
		}
	}
	result := make([]string, 0, len(lines))
	for i, line := range lines {
		if i < firstSection && len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			trimmed := strings.TrimSpace(line)
			if tomlAssignmentKey(trimmed) == key {
				continue
			}
		}
		result = append(result, line)
	}
	return result
}

func upsertTomlLine(lines []string, key, value string) []string {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if tomlAssignmentKey(trimmed) == key {
			lines[i] = key + " = " + value
			return lines
		}
	}
	return append(lines, key+" = "+value)
}

// tomlAssignmentKey returns the exact key on a simple TOML assignment line.
// Prefix matching would treat keys such as model_provider_extra as the
// managed model_provider setting and overwrite unrelated user configuration.
func tomlAssignmentKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	parts := strings.SplitN(trimmed, "=", 2)
	if len(parts) != 2 {
		return ""
	}
	key := strings.TrimSpace(parts[0])
	if key == "" || strings.ContainsAny(key, " \t") {
		return ""
	}
	return key
}

func upsertTomlSectionValue(lines []string, sectionName, key, value string) []string {
	header := "[" + sectionName + "]"
	section := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			section = i
			break
		}
	}
	if section < 0 {
		return append(lines, "", header, key+" = "+value)
	}
	end := len(lines)
	for i := section + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	block := upsertTomlLine(lines[section+1:end], key, value)
	return append(append(append([]string{}, lines[:section+1]...), block...), lines[end:]...)
}

func upsertTomlSection(lines []string, sectionName string, values []string) []string {
	header := "[" + sectionName + "]"
	section := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			section = i
			break
		}
	}
	if section < 0 {
		return append(append(lines, ""), append([]string{header}, values...)...)
	}
	end := len(lines)
	for i := section + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	block := append([]string(nil), lines[section+1:end]...)
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) == 2 {
			block = upsertTomlLine(block, strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
	return append(append(append([]string{}, lines[:section+1]...), block...), lines[end:]...)
}
