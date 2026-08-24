/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 代理 API 运行时快照与热切换状态
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package relay

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/network"
	"codexrelay/internal/storage"
	"codexrelay/internal/usage"
)

type ActiveProfile struct {
	Profile   config.Profile
	APIKey    string
	Target    *url.URL
	Transport *http.Transport
}

type State struct {
	Config      config.AppConfig
	Active      map[string]*ActiveProfile
	SystemProxy network.SystemProxyInfo
}

type Runtime struct {
	configStore    *config.Store
	usageStore     *usage.Store
	state          atomic.Pointer[State]
	mu             sync.Mutex
	nextID         atomic.Uint64
	startedAt      time.Time
	healthMu       sync.Mutex
	health         map[string]*profileHealthState
	healthChanged  func()
	resultObserved func(profileID, category string, status int, transportError bool)
}

func New(configStore *config.Store, usageStore *usage.Store, cfg config.AppConfig) (*Runtime, error) {
	runtime := &Runtime{configStore: configStore, usageStore: usageStore, startedAt: time.Now(), health: make(map[string]*profileHealthState)}
	state, err := buildState(cfg)
	if err != nil {
		return nil, err
	}
	runtime.state.Store(state)
	return runtime, nil
}

func buildState(cfg config.AppConfig) (*State, error) {
	if err := network.Validate(cfg.Network, cfg.ProxyPort); err != nil {
		return nil, err
	}
	systemInfo := network.DetectSystemProxy()
	state := &State{Config: cfg, Active: map[string]*ActiveProfile{}, SystemProxy: systemInfo}
	for category, id := range cfg.ActiveProfiles {
		index := config.FindProfileIndex(cfg.Profiles, id)
		if index < 0 {
			return nil, fmt.Errorf("类别 %q 启用的代理 API 不存在", category)
		}
		selected := cfg.Profiles[index]
		if selected.Category != category {
			return nil, fmt.Errorf("类别 %q 的启用代理 API 类别不匹配", category)
		}
		if err := config.ValidateProfile(selected); err != nil {
			return nil, err
		}
		if strings.TrimSpace(selected.APIKey) == "" {
			return nil, fmt.Errorf("类别 %q 当前代理 API 没有 API 密钥", category)
		}
		target, _ := url.Parse(selected.BaseURL)
		transport, err := network.BuildTransport(cfg.Network, systemInfo, cfg.ProxyPort)
		if err != nil {
			return nil, err
		}
		state.Active[category] = &ActiveProfile{
			Profile: config.CloneProfile(selected), APIKey: selected.APIKey,
			Target: target, Transport: transport,
		}
	}
	return state, nil
}

func (r *Runtime) State() *State {
	return r.state.Load()
}

// DataDirectory 返回当前配置和用量文件所在目录；运行目录迁移期间由 Runtime 锁保护。
func (r *Runtime) DataDirectory() string {
	return filepath.Dir(r.configStore.Path())
}

// MigrateDataDirectory 在运行时锁内复制当前快照、提交外部路径指针并切换两个 Store。
// 目标目录中的同名文件不会被覆盖；提交失败时保留旧目录并清理本次新文件。
func (r *Runtime) MigrateDataDirectory(target string, commit func() error) (string, error) {
	target = filepath.Clean(strings.TrimSpace(target))
	if target == "" || !filepath.IsAbs(target) {
		return "", errors.New("CodexRelay 数据目录必须是绝对路径")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.state.Load()
	if state == nil {
		return "", errors.New("程序尚未初始化")
	}
	oldDirectory := filepath.Dir(r.configStore.Path())
	if filepath.Clean(oldDirectory) == target {
		return oldDirectory, nil
	}
	configPath := filepath.Join(target, "config.json")
	usagePath := filepath.Join(target, "usage.json")
	for _, path := range []string{configPath, usagePath} {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("目标数据目录已存在 %s，拒绝覆盖", filepath.Base(path))
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("检查目标数据文件失败: %w", err)
		}
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return "", fmt.Errorf("创建目标数据目录失败: %w", err)
	}
	if err := storage.WriteJSONAtomic(configPath, ".config-*.tmp", state.Config); err != nil {
		return "", fmt.Errorf("迁移配置失败: %w", err)
	}
	if err := storage.WriteJSONAtomic(usagePath, ".usage-*.tmp", r.usageStore.Snapshot()); err != nil {
		_ = os.Remove(configPath)
		return "", fmt.Errorf("迁移用量统计失败: %w", err)
	}
	if commit != nil {
		if err := commit(); err != nil {
			_ = os.Remove(configPath)
			_ = os.Remove(usagePath)
			return "", err
		}
	}
	r.configStore.SetPath(configPath)
	r.usageStore.SetPath(usagePath)
	return oldDirectory, nil
}

func (r *Runtime) UpdateConfig(mutator func(*config.AppConfig) error) (config.AppConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.state.Load()
	if current == nil {
		return config.AppConfig{}, errors.New("程序尚未初始化")
	}
	next := config.Clone(current.Config)
	if err := mutator(&next); err != nil {
		return config.AppConfig{}, err
	}
	state, err := buildState(next)
	if err != nil {
		return config.AppConfig{}, err
	}
	if err := r.configStore.Save(next); err != nil {
		return config.AppConfig{}, err
	}
	r.state.Store(state)
	return next, nil
}

func (r *Runtime) Uptime() time.Duration {
	return time.Since(r.startedAt)
}

func (r *Runtime) RecentRecords() []usage.RequestRecord {
	return r.usageStore.Snapshot().Recent
}

func (r *Runtime) UsageOverview() usage.Overview {
	return r.usageStore.Overview()
}

func (r *Runtime) ClearUsage() error {
	return r.usageStore.Clear()
}

// ActivatePortablePersistence 在首次引导完成后启用两个便携存储的后续写入；每个文件仍通过独立原子写入落盘。
// 配置快照先写入，若用量文件写入失败，下一次调用仍会重试未完成的部分。
func (r *Runtime) ActivatePortablePersistence() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.state.Load()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	if err := r.configStore.ActivatePersistence(state.Config); err != nil {
		return err
	}
	return r.usageStore.ActivatePersistence()
}

func (r *Runtime) record(record usage.RequestRecord) error {
	return r.usageStore.Record(record)
}
