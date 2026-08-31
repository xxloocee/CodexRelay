/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Claude 配置文件的环境变量和模型写入
 * @File          : Claude 客户端适配器
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"encoding/json"
	"fmt"

	"codexrelay/internal/config"
)

// configureClaude 写入已确认的 Claude 环境变量；同一模型目录的默认项用于四个角色默认值。
func configureClaude(path, endpoint, key string, models []config.ModelEntry, defaultModel string) error {
	_, err := configureSingleResult(path, func(existing []byte) ([]byte, error) {
		return renderClaude(existing, endpoint, key, models, defaultModel)
	})
	return err
}

func renderClaude(existing []byte, endpoint, key string, models []config.ModelEntry, defaultModel string) ([]byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, err
	}
	value := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &value); err != nil {
			return nil, fmt.Errorf("解析 Claude settings.json 失败，请手动配置: %w", err)
		}
	}
	env, ok := value["env"].(map[string]any)
	if !ok {
		env = map[string]any{}
		value["env"] = env
	}
	env["ANTHROPIC_BASE_URL"] = endpoint
	env["ANTHROPIC_AUTH_TOKEN"] = key
	modelNames := []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL"}
	if model := selectedModelID(models, defaultModel); model != "" {
		for _, name := range modelNames {
			env[name] = model
		}
	} else if models != nil {
		// 非 nil 的空目录表示用户明确清空模型；只移除由 CodexRelay
		// 管理的 Claude 模型变量，保留其他环境变量和配置结构。
		for _, name := range modelNames {
			delete(env, name)
		}
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码 Claude settings.json 失败: %w", err)
	}
	return append(data, '\n'), nil
}
