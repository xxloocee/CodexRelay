/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 桌面层客户端配置绑定与配置持久化调度
 * @File          : 外部客户端配置桌面入口
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
)

// PublicClientConfig 是高级设置和启用前检查使用的脱敏状态，不返回外部配置正文。
// 类型别名保持 Wails 绑定中的公开字段和 JSON 形状不变。
type PublicClientConfig = clientconfig.PublicClientConfig

func publicClientConfigs(cfg config.AppConfig) []PublicClientConfig {
	return clientconfig.PublicConfigs(cfg)
}

// scanClientConfigs 在启动阶段记录已安装客户端的已知配置目录；失败只返回错误，不修改外部客户端文件。
func (s *DesktopService) scanClientConfigs() error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	paths, changed := clientconfig.DiscoverConfigPaths(state.Config)
	if !changed {
		return nil
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.ClientConfigs = paths
		return nil
	})
}

// CheckClientConfig 返回某一分类当前配置文件的实时状态；它只读本地文件，不访问上游。
func (s *DesktopService) CheckClientConfig(category string) (PublicClientConfig, error) {
	state := s.runtime.State()
	if state == nil {
		return PublicClientConfig{}, errors.New("程序尚未初始化")
	}
	return clientconfig.Inspect(state.Config, strings.TrimSpace(category))
}

// SetClientConfigPath 保存外部客户端配置目录，并立即返回新的本地检测状态。
func (s *DesktopService) SetClientConfigPath(category, directory string) error {
	category = strings.TrimSpace(category)
	directory = strings.TrimSpace(directory)
	configFile, ok := clientconfig.ConfigFileFor(category)
	if !ok {
		return errors.New("未知 API 类别")
	}
	if !clientconfig.Supports(category) {
		return errors.New("该 API 类别没有可自动配置的外部客户端")
	}
	if directory == "" {
		return errors.New("配置目录不能为空")
	}
	if !filepath.IsAbs(directory) {
		return errors.New("配置目录必须是绝对路径")
	}
	if err := clientconfig.ValidateExternalClientDirectory(s.runtime.DataDirectory(), directory); err != nil {
		return err
	}
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	entry := state.Config.ClientConfigs[category]
	if filepath.Clean(entry.ConfigDir) != filepath.Clean(directory) {
		// A Relay-managed directory (including legacy state without a persisted
		// snapshot) cannot be orphaned by merely changing the pointer. The old
		// files and any restore metadata must remain tied to the old directory.
		if entry.Mode == "relay" || len(entry.OfficialBackups) > 0 {
			return errors.New("客户端仍由 CodexRelay 接管，请先切回官方配置后再修改目录")
		}
		status, inspectErr := clientconfig.Inspect(state.Config, category)
		if inspectErr != nil {
			return fmt.Errorf("检查旧客户端配置失败: %w", inspectErr)
		}
		if status.Status == "error" {
			return fmt.Errorf("检查旧客户端配置失败: %s", status.Error)
		}
		if status.Configured {
			return errors.New("客户端当前文件仍是 CodexRelay 配置，请先切回官方配置后再修改目录")
		}
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		if cfg.ClientConfigs == nil {
			cfg.ClientConfigs = map[string]config.ClientConfig{}
		}
		entry := cfg.ClientConfigs[category]
		entry.ConfigDir = filepath.Clean(directory)
		entry.ConfigFile = configFile
		cfg.ClientConfigs[category] = entry
		if err := clientconfig.ValidateClientConfigTargets(cfg.ClientConfigs); err != nil {
			return err
		}
		return nil
	})
}

// SetClientConfigSkip 保存某一外部客户端是否跳过主界面切换前的配置检查；它不改写外部文件。
func (s *DesktopService) SetClientConfigSkip(category string, skip bool) error {
	category = strings.TrimSpace(category)
	if !clientconfig.Supports(category) {
		return errors.New("该 API 类别没有可自动配置的外部客户端")
	}
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	return s.updateConfig(func(cfg *config.AppConfig) error {
		if cfg.ClientConfigs == nil {
			cfg.ClientConfigs = map[string]config.ClientConfig{}
		}
		entry := cfg.ClientConfigs[category]
		entry.SkipConfigReplacement = skip
		if entry.ConfigFile == "" {
			if filename, ok := clientconfig.ConfigFileFor(category); ok {
				entry.ConfigFile = filename
			}
		}
		cfg.ClientConfigs[category] = entry
		return nil
	})
}

// ConfigureClient 备份并写入当前分类的 CodexRelay 地址和本地访问令牌。
// 它不改变令牌启用映射，调用方完成写入后再执行启用，避免配置失败却显示为已切换。
func (s *DesktopService) ConfigureClient(category, profileID string) error {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	category = strings.TrimSpace(category)
	profileID = strings.TrimSpace(profileID)
	result, err := clientconfig.ConfigureWithResult(state.Config, category, profileID, s.runtime.DataDirectory())
	if err != nil {
		return err
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		rememberOfficialConfig(cfg, category, result)
		return nil
	}); err != nil {
		if result.Rollback != nil {
			if rollbackErr := result.Rollback(); rollbackErr != nil {
				return fmt.Errorf("保存客户端配置状态失败: %v；外部配置回退失败: %w", err, rollbackErr)
			}
		}
		return err
	}
	s.notifyStateChanged()
	return nil
}

// ConfigureDetectedClients 在用户明确确认后批量接管已检测到的外部客户端。
// 它只处理配置注册表中已有目录且当前可读的客户端，不会创建未检测到的
// 配置文件；任一客户端失败时恢复此前已写入的全部文件。
func (s *DesktopService) ConfigureDetectedClients() error {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	results := make([]clientconfig.ConfigureResult, 0)
	officialResults := make(map[string]clientconfig.ConfigureResult)
	for _, category := range config.Categories {
		if !clientconfig.Supports(category) {
			continue
		}
		entry := state.Config.ClientConfigs[category]
		if strings.TrimSpace(entry.ConfigDir) == "" || entry.SkipConfigReplacement {
			continue
		}
		status, err := clientconfig.Inspect(state.Config, category)
		if err != nil {
			return clientConfigRollbackError(fmt.Errorf("检查 %s 客户端配置失败: %w", category, err), results)
		}
		if status.Status == "error" {
			return clientConfigRollbackError(fmt.Errorf("检查 %s 客户端配置失败: %s", category, status.Error), results)
		}
		if !status.Detected {
			continue
		}
		if strings.TrimSpace(state.Config.ActiveProfiles[category]) == "" {
			continue
		}
		result, err := clientconfig.ConfigureWithResult(state.Config, category, state.Config.ActiveProfiles[category], s.runtime.DataDirectory())
		if err != nil {
			if rollbackErr := rollbackClientConfigs(results); rollbackErr != nil {
				return fmt.Errorf("配置 %s 客户端失败: %v；此前客户端配置回退失败: %w", category, err, rollbackErr)
			}
			return fmt.Errorf("配置 %s 客户端失败: %w", category, err)
		}
		results = append(results, result)
		officialResults[category] = result
	}
	if len(officialResults) > 0 {
		if err := s.updateConfig(func(cfg *config.AppConfig) error {
			for category, result := range officialResults {
				rememberOfficialConfig(cfg, category, result)
			}
			return nil
		}); err != nil {
			if rollbackErr := rollbackClientConfigs(results); rollbackErr != nil {
				return fmt.Errorf("保存客户端配置状态失败: %v；外部配置回退失败: %w", err, rollbackErr)
			}
			return err
		}
	}
	s.notifyStateChanged()
	return nil
}
