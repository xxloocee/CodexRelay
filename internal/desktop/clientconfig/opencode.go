/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : OpenCode JSON/JSON5 provider 配置写入
 * @File          : OpenCode 客户端适配器
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"fmt"
	"strings"

	"codexrelay/internal/config"
)

func configureOpenCode(path, endpoint, key string, models []config.ModelEntry, defaultModel string) error {
	_, err := configureSingleResult(path, func(existing []byte) ([]byte, error) {
		return renderOpenCode(existing, endpoint, key, models, defaultModel)
	})
	return err
}

func renderOpenCode(existing []byte, endpoint, key string, models []config.ModelEntry, defaultModel string) ([]byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, err
	}
	value, err := readJSONObjectData(existing)
	if err != nil {
		return nil, fmt.Errorf("解析 OpenCode 配置失败，请手动配置: %w", err)
	}
	providers, ok := value["provider"].(map[string]any)
	if !ok {
		providers = map[string]any{}
		value["provider"] = providers
	}
	provider, _ := providers["codexrelay"].(map[string]any)
	if provider == nil {
		provider = map[string]any{}
	}
	provider["npm"] = "@ai-sdk/openai-compatible"
	provider["name"] = "CodexRelay"
	options, _ := provider["options"].(map[string]any)
	if options == nil {
		options = map[string]any{}
		provider["options"] = options
	}
	options["baseURL"] = endpoint
	options["apiKey"] = key
	if len(models) > 0 {
		modelMap := map[string]any{}
		for _, model := range models {
			name := strings.TrimSpace(model.Name)
			if name == "" {
				name = model.ID
			}
			modelMap[model.ID] = map[string]any{"name": name}
		}
		provider["models"] = modelMap
	} else if models != nil {
		delete(provider, "models")
	}
	providers["codexrelay"] = provider
	if selected := selectedModelID(models, defaultModel); selected != "" {
		// OpenCode selects the active provider/model through the root model key;
		// registering models alone leaves an existing provider active.
		value["model"] = "codexrelay/" + selected
	} else if models != nil {
		if model, ok := value["model"].(string); ok && strings.HasPrefix(model, "codexrelay/") {
			delete(value, "model")
		}
	}
	data, err := marshalJSONObject(value)
	if err != nil {
		return nil, fmt.Errorf("编码 OpenCode 配置失败: %w", err)
	}
	return data, nil
}
