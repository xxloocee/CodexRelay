/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : OpenClaw JSON/JSON5 provider 和默认模型配置写入
 * @File          : OpenClaw 客户端适配器
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"fmt"
	"strings"

	"codexrelay/internal/config"
)

func configureOpenClaw(path, endpoint, key string, catalog []config.ModelEntry, defaultModel string) error {
	_, err := configureSingleResult(path, func(existing []byte) ([]byte, error) {
		return renderOpenClaw(existing, endpoint, key, catalog, defaultModel)
	})
	return err
}

func renderOpenClaw(existing []byte, endpoint, key string, catalog []config.ModelEntry, defaultModel string) ([]byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, err
	}
	value, err := readJSONObjectData(existing)
	if err != nil {
		return nil, fmt.Errorf("解析 OpenClaw JSON/JSON5 配置失败，请手动配置: %w", err)
	}
	modelRoot, ok := value["models"].(map[string]any)
	if !ok {
		modelRoot = map[string]any{}
		value["models"] = modelRoot
	}
	providers, ok := modelRoot["providers"].(map[string]any)
	if !ok {
		providers = map[string]any{}
		modelRoot["providers"] = providers
	}
	// Merge into an existing provider so a profile without a cached model
	// catalog never erases models previously configured by the user.
	provider, _ := providers["codexrelay"].(map[string]any)
	if provider == nil {
		provider = map[string]any{}
	}
	provider["baseUrl"] = endpoint
	provider["api"] = "openai-completions"
	provider["apiKey"] = key
	if len(catalog) > 0 {
		modelEntries := make([]any, 0, len(catalog))
		for _, model := range catalog {
			name := strings.TrimSpace(model.Name)
			if name == "" {
				name = model.ID
			}
			entry := map[string]any{"id": model.ID, "name": name}
			if model.ContextWindow > 0 {
				entry["contextWindow"] = model.ContextWindow
			}
			modelEntries = append(modelEntries, entry)
		}
		provider["models"] = modelEntries
	}
	providers["codexrelay"] = provider
	if len(catalog) > 0 {
		if defaultModel == "" || !containsModel(catalog, defaultModel) {
			defaultModel = catalog[0].ID
		}
		agents, ok := value["agents"].(map[string]any)
		if !ok {
			agents = map[string]any{}
			value["agents"] = agents
		}
		defaults, ok := agents["defaults"].(map[string]any)
		if !ok {
			defaults = map[string]any{}
			agents["defaults"] = defaults
		}
		modelConfig, ok := defaults["model"].(map[string]any)
		if !ok {
			modelConfig = map[string]any{}
			defaults["model"] = modelConfig
		}
		modelConfig["primary"] = "codexrelay/" + defaultModel
		allowedModels, ok := defaults["models"].(map[string]any)
		if !ok {
			allowedModels = map[string]any{}
			defaults["models"] = allowedModels
		}
		for id := range allowedModels {
			if strings.HasPrefix(id, "codexrelay/") {
				delete(allowedModels, id)
			}
		}
		for _, model := range catalog {
			allowedModels["codexrelay/"+model.ID] = map[string]any{}
		}
	} else if catalog != nil {
		delete(provider, "models")
		if agents, ok := value["agents"].(map[string]any); ok {
			if defaults, ok := agents["defaults"].(map[string]any); ok {
				if modelConfig, ok := defaults["model"].(map[string]any); ok {
					if primary, ok := modelConfig["primary"].(string); ok && strings.HasPrefix(primary, "codexrelay/") {
						delete(modelConfig, "primary")
					}
				}
				if allowedModels, ok := defaults["models"].(map[string]any); ok {
					for id := range allowedModels {
						if strings.HasPrefix(id, "codexrelay/") {
							delete(allowedModels, id)
						}
					}
				}
			}
		}
	}
	data, err := marshalJSONObject(value)
	if err != nil {
		return nil, fmt.Errorf("编码 OpenClaw 配置失败: %w", err)
	}
	return data, nil
}
