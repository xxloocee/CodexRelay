package desktop

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
	"codexrelay/internal/network"
	"codexrelay/internal/platform"
)

func (s *DesktopService) SetNetwork(input network.Settings) error {
	input.Mode = strings.TrimSpace(input.Mode)
	input.ProxyURL = strings.TrimSpace(input.ProxyURL)
	return s.updateConfig(func(cfg *config.AppConfig) error {
		if err := network.Validate(input, cfg.ProxyPort); err != nil {
			return err
		}
		cfg.Network = input
		return nil
	})
}

// SetProxyPort 校验并热切换本地代理监听端口；新端口无法绑定时不修改配置和现有监听。
// 端口范围为 TCP 的 1-65535；成功后新请求地址立即使用新端口，已有连接由旧服务优雅退出。
func (s *DesktopService) SetProxyPort(port int) error {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	if port < 1 || port > 65535 {
		return errors.New("监听端口必须是 1 到 65535 之间的整数")
	}
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	if state.Config.ProxyPort == port {
		return nil
	}
	listener, server, err := s.prepareProxyListener(port, state.Config.ListenOnAllInterfaces)
	if err != nil {
		return err
	}
	nextConfig := state.Config
	nextConfig.ProxyPort = port
	clientResults, err := syncManagedClientConfigs(state.Config, nextConfig)
	if err != nil {
		_ = listener.Close()
		return err
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.ProxyPort = port
		return nil
	}); err != nil {
		_ = listener.Close()
		if rollbackErr := rollbackClientConfigs(clientResults); rollbackErr != nil {
			return fmt.Errorf("保存监听端口失败: %v；客户端配置回退失败: %w", err, rollbackErr)
		}
		return err
	}
	if !s.installProxyListener(server, listener) {
		restoreConfigErr := s.updateConfig(func(cfg *config.AppConfig) error {
			cfg.ProxyPort = state.Config.ProxyPort
			return nil
		})
		rollbackErr := rollbackClientConfigs(clientResults)
		_ = listener.Close()
		if restoreConfigErr != nil || rollbackErr != nil {
			return fmt.Errorf("代理监听切换失败；本地配置恢复错误: %v；客户端配置回退错误: %v", restoreConfigErr, rollbackErr)
		}
		return errors.New("代理监听切换失败，已恢复原配置")
	}
	return nil
}

// SetProxyListenAllInterfaces 切换透明代理是否监听所有 IPv4 网卡；绑定失败时保留原配置和监听。
// 开启后 WSL2 可通过 Windows 主机地址访问，但所有请求仍须通过本地访问令牌认证。
func (s *DesktopService) SetProxyListenAllInterfaces(enabled bool) error {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("代理尚未初始化")
	}
	if state.Config.ListenOnAllInterfaces == enabled {
		return nil
	}
	if !enabled && !config.IsLoopbackClientAccessHost(state.Config.ClientAccessHost) {
		return errors.New("当前客户端访问主机不是回环地址，请先改回回环地址后再关闭允许 WSL2 访问")
	}
	oldServer, server, listener, err := s.rebindProxyListenScope(state.Config.ProxyPort, state.Config.ListenOnAllInterfaces, enabled)
	if err != nil {
		return err
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.ListenOnAllInterfaces = enabled
		return nil
	}); err != nil {
		_ = listener.Close()
		if restoreErr := s.restoreProxyListener(state.Config.ProxyPort, state.Config.ListenOnAllInterfaces); restoreErr != nil {
			return fmt.Errorf("%v；恢复原监听失败: %w", err, restoreErr)
		}
		shutdownProxyServerAsync(oldServer)
		return err
	}
	s.attachProxyListener(server, listener)
	shutdownProxyServerAsync(oldServer)
	return nil
}

// syncManagedClientConfigs updates only clients whose current file is already
// configured for CodexRelay. It uses the prospective network settings while
// preserving the current settings for the status check, so changing a port or
// listen scope cannot silently take over an unrelated client configuration.
func syncManagedClientConfigs(previous, next config.AppConfig) ([]clientconfig.ConfigureResult, error) {
	results := make([]clientconfig.ConfigureResult, 0)
	for _, category := range config.Categories {
		if !clientconfig.Supports(category) {
			continue
		}
		entry := previous.ClientConfigs[category]
		if entry.SkipConfigReplacement || (strings.TrimSpace(previous.ActiveProfiles[category]) == "" && clientconfig.RequiresProfile(category)) {
			continue
		}
		status, err := clientconfig.Inspect(previous, category)
		if err != nil {
			return nil, clientConfigRollbackError(fmt.Errorf("检查 %s 客户端配置失败: %w", category, err), results)
		}
		if status.Status == "error" {
			return nil, clientConfigRollbackError(fmt.Errorf("检查 %s 客户端配置失败: %s", category, status.Error), results)
		}
		if !status.Configured {
			continue
		}
		result, err := clientconfig.ConfigureWithResult(next, category, next.ActiveProfiles[category])
		if err != nil {
			if rollbackErr := rollbackClientConfigs(results); rollbackErr != nil {
				return nil, fmt.Errorf("同步 %s 客户端配置失败: %v；此前客户端配置回退失败: %w", category, err, rollbackErr)
			}
			return nil, fmt.Errorf("同步 %s 客户端配置失败: %w", category, err)
		}
		results = append(results, result)
	}
	return results, nil
}

// SetClientAccessHost 更新写入外部客户端的访问主机。监听端口和类别路径由
// config.json 的其他字段生成；已接管客户端在本地保存成功前统一同步并可回退。
func (s *DesktopService) SetClientAccessHost(raw string) error {
	host, err := config.NormalizeClientAccessHost(raw)
	if err != nil {
		return err
	}
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	if state.Config.ClientAccessHost == host {
		return nil
	}
	if !state.Config.ListenOnAllInterfaces && !config.IsLoopbackClientAccessHost(host) {
		return errors.New("代理仅监听本机时，客户端访问主机必须是回环地址；如需使用局域网地址，请先开启允许 WSL2 访问")
	}
	next := state.Config
	next.ClientAccessHost = host
	results, err := syncManagedClientConfigs(state.Config, next)
	if err != nil {
		return err
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.ClientAccessHost = host
		return nil
	}); err != nil {
		return clientConfigRollbackError(fmt.Errorf("保存客户端访问主机失败: %w", err), results)
	}
	return nil
}

func rollbackClientConfigs(results []clientconfig.ConfigureResult) error {
	var firstErr error
	for index := len(results) - 1; index >= 0; index-- {
		if results[index].Rollback == nil {
			continue
		}
		if err := results[index].Rollback(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func clientConfigRollbackError(original error, results []clientconfig.ConfigureResult) error {
	if rollbackErr := rollbackClientConfigs(results); rollbackErr != nil {
		return fmt.Errorf("%v；此前客户端配置回退失败: %w", original, rollbackErr)
	}
	return original
}

func (s *DesktopService) SetPreferences(input config.Preferences) error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	previous := state.Config.Preferences.LaunchAtStartup
	if err := platform.SetLaunchAtStartup(input.LaunchAtStartup); err != nil {
		return fmt.Errorf("更新开机启动失败: %w", err)
	}
	_, err := s.runtime.UpdateConfig(func(cfg *config.AppConfig) error {
		cfg.Preferences = input
		return nil
	})
	if err != nil {
		_ = platform.SetLaunchAtStartup(previous)
		return err
	}
	s.notifyStateChanged()
	return nil
}

// SetTaskNotification 保存独立任务完成通知的完整访问 URL 和重试设置。
// 修改只影响后台 watcher；它不写入 Codex 的 config.toml、hooks.json 或系统代理。
func (s *DesktopService) SetTaskNotification(input config.TaskNotification) error {
	input.WebhookURL = strings.TrimSpace(input.WebhookURL)
	// 设置页提交代表用户已明确选择事件范围，允许其关闭全部事件而不被默认值覆盖。
	input.EventsInitialized = true
	input = config.NormalizeTaskNotification(input)
	if err := config.ValidateTaskNotification(input); err != nil {
		return err
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.TaskNotification = input
		return nil
	})
}

// TestTaskNotification 由用户在设置页确认后测试当前 Webhook。测试请求不包含
// rollout、任务标识、路径、prompt 或最终回复，失败不会改变 pending/outbox 队列。
func (s *DesktopService) TestTaskNotification() error {
	if s.taskNotifier == nil {
		return errors.New("任务通知服务尚未初始化")
	}
	if err := s.taskNotifier.Test(context.Background()); err != nil {
		return fmt.Errorf("测试任务通知失败: %w", err)
	}
	return nil
}

// SetTokenSwitchSettings 保存所有来源共用的故障触发、阈值和候选循环策略。
// 关闭或重新开启某个错误类型会清理其旧统计；阈值、窗口和模式变化则基于仍有效的其他统计重新评估。
func (s *DesktopService) SetTokenSwitchSettings(input config.TokenSwitchSettings) error {
	if err := config.ValidateTokenSwitch(input); err != nil {
		return err
	}
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	previous := state.Config.TokenSwitch
	if _, err := s.runtime.UpdateConfig(func(cfg *config.AppConfig) error {
		cfg.TokenSwitch = input
		return nil
	}); err != nil {
		return err
	}
	s.runtime.ClearHealthForTokenSwitchChanges(previous, input)
	if input.Mode != config.TokenSwitchModeAuto {
		// 自动轮次只在本次运行的自动模式中有效；切回手动后必须丢弃尝试集合和自动通知。
		s.switchMu.Lock()
		s.switchRounds = make(map[string]*tokenSwitchRound)
		s.autoSwitchNotices = make(map[string]*PublicDogeTokenSwitchPrompt)
		s.switchMu.Unlock()
	}
	// 修改模式或阈值后重新评估已有运行时状态，使自动模式无需等待下一次请求才接管。
	s.handleHealthChanged()
	return nil
}

// SetDogeAlertSettings 保存余额和套餐提醒的独立开关与美元阈值。
// 关闭提醒不会删除已同步的余额、套餐或公告数据，只是不再生成对应右下角提醒。
func (s *DesktopService) SetDogeAlertSettings(input config.DogeAlertSettings) error {
	if math.IsNaN(input.BalanceThresholdUSD) || math.IsInf(input.BalanceThresholdUSD, 0) || input.BalanceThresholdUSD <= 0 {
		return errors.New("余额提醒阈值必须是大于 0 的数字")
	}
	if math.IsNaN(input.SubscriptionThresholdUSD) || math.IsInf(input.SubscriptionThresholdUSD, 0) || input.SubscriptionThresholdUSD <= 0 {
		return errors.New("套餐提醒阈值必须是大于 0 的数字")
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.Doge.Notifications.BalanceAlertEnabled = input.BalanceEnabled
		cfg.Doge.Notifications.BalanceAlertThresholdUSD = input.BalanceThresholdUSD
		cfg.Doge.Notifications.SubscriptionAlertEnabled = input.SubscriptionEnabled
		cfg.Doge.Notifications.SubscriptionAlertThresholdUSD = input.SubscriptionThresholdUSD
		reconcileDogeQuotaAlertRecords(&cfg.Doge.Notifications, cfg.Doge.Account, cfg.Doge.Subscriptions, time.Now())
		return nil
	})
}

// OpenDogeTopup 使用系统默认浏览器打开二狗子购买入口。
// 购买地址来自最近一次 `/api/user/topup/info` 同步结果，不接受前端传入的任意 URL。
func (s *DesktopService) OpenDogeTopup() error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	topupLink := strings.TrimSpace(state.Config.Doge.Topup.TopupLink)
	if topupLink == "" {
		return errors.New("二狗子暂未提供购买入口")
	}
	return platform.OpenURL(topupLink)
}

// OpenDogeProfile 使用系统默认浏览器打开二狗子用户中心，令牌生成路径由界面说明固定提示。
func (s *DesktopService) OpenDogeProfile() error {
	return platform.OpenURL(dogeProfileURL)
}

// OpenExternalURL 使用系统默认浏览器打开前端传入的外部 HTTP(S) 地址。
// URL 的协议和主机校验由 platform.OpenURL 统一执行，避免 WebView 在应用内处理外链，
// 也避免把任意协议交给操作系统。
func (s *DesktopService) OpenExternalURL(raw string) error {
	return platform.OpenURL(raw)
}
