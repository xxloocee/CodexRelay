/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 外部客户端 JSON、JSON5 配置读写辅助
 * @File          : JSON 配置辅助
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	json5 "github.com/titanous/json5"
)

func configureJSONEnv(path, endpoint, key, baseKey, tokenKey string) error {
	_, err := configureSingleResult(path, func(existing []byte) ([]byte, error) {
		return renderJSONEnv(existing, endpoint, key, baseKey, tokenKey)
	})
	return err
}

func renderJSONEnv(existing []byte, endpoint, key, baseKey, tokenKey string) ([]byte, error) {
	if err := validateExternalValue("代理地址", endpoint); err != nil {
		return nil, err
	}
	if err := validateExternalValue("访问令牌", key); err != nil {
		return nil, err
	}
	value := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &value); err != nil {
			return nil, fmt.Errorf("解析 JSON 配置失败，请手动配置: %w", err)
		}
	}
	env, ok := value["env"].(map[string]any)
	if !ok {
		env = map[string]any{}
		value["env"] = env
	}
	env[baseKey] = endpoint
	env[tokenKey] = key
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码 JSON 配置失败: %w", err)
	}
	return append(data, '\n'), nil
}

// readJSONObject 同时接受 JSON 和客户端实际使用的 JSON5 配置；不存在时返回空对象。
func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return readJSONObjectData(data)
}

func readJSONObjectData(data []byte) (map[string]any, error) {
	value := map[string]any{}
	if len(data) == 0 {
		return value, nil
	}
	if err := json.Unmarshal(data, &value); err != nil {
		value = map[string]any{}
		if json5Err := json5.Unmarshal(data, &value); json5Err != nil {
			return nil, fmt.Errorf("JSON: %v; JSON5: %w", err, json5Err)
		}
	}
	return value, nil
}

func writeJSONObject(path string, value map[string]any) error {
	data, err := marshalJSONObject(value)
	if err != nil {
		return err
	}
	return writeClientFile(path, data)
}

func marshalJSONObject(value map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
