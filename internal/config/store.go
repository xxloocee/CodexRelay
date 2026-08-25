/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : config.json 当前格式读写
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package config

import (
	"fmt"
	"sync"

	"codexrelay/internal/storage"
)

type Store struct {
	mu      sync.Mutex
	path    string
	persist bool
}

func NewStore(path string) *Store {
	return &Store{path: path, persist: true}
}

// NewDeferredStore 创建首次引导期间使用的内存优先配置存储；读取已有文件，但在引导完成前不写回便携文件。
func NewDeferredStore(path string) *Store {
	return &Store{path: path, persist: false}
}

// Path 返回当前配置文件路径；路径只在运行时目录迁移时读取，调用方不得据此恢复旧目录。
func (s *Store) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

// SetPath 在运行时目录迁移提交后切换后续配置写入目标；不会改变持久化开关。
func (s *Store) SetPath(path string) {
	s.mu.Lock()
	s.path = path
	s.mu.Unlock()
}

func (s *Store) LoadOrCreate(proxyPort int) (AppConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg AppConfig
	exists, err := storage.ReadJSON(s.path, &cfg)
	if err != nil {
		return AppConfig{}, fmt.Errorf("加载配置: %w", err)
	}
	if !exists {
		cfg = Default(proxyPort)
		if !s.persist {
			return cfg, nil
		}
		return cfg, s.saveLocked(cfg)
	}
	if cfg.Doge.BaseURL == "" {
		cfg.Doge.BaseURL = "https://api.ergouzi.life"
	}
	if cfg.Doge.SyncIntervalMinutes == 0 {
		cfg.Doge.SyncIntervalMinutes = DefaultDogeSyncIntervalMinutes
	}
	if cfg.TokenSwitch.Mode == "" {
		cfg.TokenSwitch = DefaultTokenSwitchSettings()
	} else {
		if cfg.TokenSwitch.AuthFailureThreshold == 0 {
			cfg.TokenSwitch.AuthFailureThreshold = DefaultAuthFailureThreshold
		}
		if cfg.TokenSwitch.UpstreamFailureThreshold == 0 {
			cfg.TokenSwitch.UpstreamFailureThreshold = DefaultUpstreamFailureThreshold
		}
		if cfg.TokenSwitch.UpstreamFailureWindowMinutes == 0 {
			cfg.TokenSwitch.UpstreamFailureWindowMinutes = DefaultUpstreamFailureWindowMinutes
		}
	}
	if cfg.Doge.Groups == nil {
		cfg.Doge.Groups = []string{}
	}
	if cfg.Doge.Tokens == nil {
		cfg.Doge.Tokens = []DogeToken{}
	}
	if cfg.Doge.Notifications.Announcements == nil {
		cfg.Doge.Notifications.Announcements = []DogeAnnouncement{}
	}
	if cfg.Doge.Notifications.ReadAnnouncementIDs == nil {
		cfg.Doge.Notifications.ReadAnnouncementIDs = []int64{}
	}
	if cfg.Doge.Notifications.DismissedAlertKeys == nil {
		cfg.Doge.Notifications.DismissedAlertKeys = []string{}
	}
	if cfg.Doge.Notifications.BalanceAlertRecords == nil {
		cfg.Doge.Notifications.BalanceAlertRecords = []DogeBalanceAlertRecord{}
	}
	if cfg.Doge.Notifications.SubscriptionAlertRecords == nil {
		cfg.Doge.Notifications.SubscriptionAlertRecords = []DogeSubscriptionAlertRecord{}
	}
	if cfg.Doge.Notifications.BalanceAlertThresholdUSD <= 0 {
		cfg.Doge.Notifications.BalanceAlertEnabled = true
		cfg.Doge.Notifications.BalanceAlertThresholdUSD = DefaultQuotaAlertThresholdUSD
	}
	if cfg.Doge.Notifications.SubscriptionAlertThresholdUSD <= 0 {
		cfg.Doge.Notifications.SubscriptionAlertEnabled = true
		cfg.Doge.Notifications.SubscriptionAlertThresholdUSD = DefaultQuotaAlertThresholdUSD
	}
	if cfg.ClientConfigs == nil {
		cfg.ClientConfigs = map[string]ClientConfig{}
	}
	if cfg.Preferences.VisibleCategories == nil {
		cfg.Preferences.VisibleCategories = append([]string(nil), Categories...)
	}
	if cfg.Preferences.RestoreViewMode == "" {
		cfg.Preferences.RestoreViewMode = RestoreViewCurrent
	}
	if cfg.TaskNotification.IdleGraceSeconds == 0 {
		cfg.TaskNotification.IdleGraceSeconds = DefaultTaskNotificationIdleGraceSeconds
	}
	if cfg.TaskNotification.RequestTimeoutSeconds == 0 {
		cfg.TaskNotification.RequestTimeoutSeconds = DefaultTaskNotificationRequestTimeoutSeconds
	}
	cfg.TaskNotification = NormalizeTaskNotification(cfg.TaskNotification)
	cfg.FailoverOrder = NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
	if err := Validate(cfg); err != nil {
		return AppConfig{}, err
	}
	if proxyPort != 0 {
		cfg.ProxyPort = proxyPort
	}
	return cfg, nil
}

func (s *Store) Save(cfg AppConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.persist {
		return nil
	}
	return s.saveLocked(cfg)
}

// ActivatePersistence 在首次引导完成后写入当前配置，并使后续更新恢复正常持久化。
func (s *Store) ActivatePersistence(cfg AppConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persist {
		return nil
	}
	if err := s.writeLocked(cfg); err != nil {
		return err
	}
	s.persist = true
	return nil
}

func (s *Store) saveLocked(cfg AppConfig) error {
	return s.writeLocked(cfg)
}

func (s *Store) writeLocked(cfg AppConfig) error {
	if err := storage.WriteJSONAtomic(s.path, ".config-*.tmp", cfg); err != nil {
		return fmt.Errorf("保存配置: %w", err)
	}
	return nil
}
