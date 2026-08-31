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
	"errors"
	"fmt"
	"os"
	"strings"
)

func configureCodex(configPath, authPath, endpoint, key, defaultModel string) error {
	_, err := configureCodexResult(configPath, authPath, endpoint, key, defaultModel)
	return err
}

func configureCodexResult(configPath, authPath, endpoint, key, defaultModel string) (ConfigureResult, error) {
	return applyConfigTransaction([]string{configPath, authPath}, func(snapshots map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		configData, authData, err := renderCodexData(configPath, snapshots[configPath].data, snapshots[authPath].data, endpoint, key, defaultModel)
		if err != nil {
			return nil, err
		}
		return []ConfigFileChange{{Path: configPath, Data: configData}, {Path: authPath, Data: authData}}, nil
	})
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
	auth, err := readJSONObjectData(authSource)
	if err != nil {
		return nil, nil, fmt.Errorf("解析 Codex auth.json 失败，请手动配置: %w", err)
	}
	auth["OPENAI_API_KEY"] = key
	authData, err := marshalJSONObject(auth)
	if err != nil {
		return nil, nil, fmt.Errorf("编码 Codex auth.json 失败: %w", err)
	}
	return []byte(configText), authData, nil
}
