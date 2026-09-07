//go:build windows

/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Windows GitHub Release 版本检测、校验、替换和重启
 * @File          : Windows 桌面更新实现
 */
package desktop

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"codexrelay/internal/network"
	"codexrelay/internal/relay"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

const (
	updateRepository     = "xxloocee/CodexRelay"
	updateChecksumAsset  = "SHA256SUMS"
	updateRestartEvent   = "relay-update-restart-error"
	updateRestartDelay   = 500 * time.Millisecond
	updateForceExitDelay = 20 * time.Second
)

func configureUpdater(app *application.App, relayRuntime *relay.Runtime) error {
	provider := newGitHubReleaseProvider(
		updateRepository,
		updateChecksumAsset,
		&http.Client{
			Timeout:   15 * time.Minute,
			Transport: retryRoundTripper{base: &updateRoundTripper{runtime: relayRuntime}},
		},
	)
	return app.Updater.Init(updater.Config{
		CurrentVersion: applicationVersion,
		Providers:      []updater.Provider{provider},
		Window:         updater.WindowNone,
	})
}

type updateRoundTripper struct {
	runtime   *relay.Runtime
	mu        sync.Mutex
	config    updateTransportConfig
	transport *http.Transport
}

type updateTransportConfig struct {
	settings    network.Settings
	systemProxy network.SystemProxyInfo
	proxyPort   int
}

func (transport *updateRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	state := transport.runtime.State()
	if state == nil {
		return nil, errors.New("程序尚未初始化")
	}
	config := updateTransportConfig{
		settings:    state.Config.Network,
		systemProxy: network.DetectSystemProxy(),
		proxyPort:   state.Config.ProxyPort,
	}

	transport.mu.Lock()
	if transport.transport == nil || transport.config != config {
		current, err := network.BuildTransport(config.settings, config.systemProxy, config.proxyPort)
		if err != nil {
			transport.mu.Unlock()
			return nil, err
		}
		previous := transport.transport
		transport.transport = current
		transport.config = config
		if previous != nil {
			previous.CloseIdleConnections()
		}
	}
	current := transport.transport
	transport.mu.Unlock()

	response, err := current.RoundTrip(request)
	if err != nil && config.settings.Mode == "system" && config.systemProxy.PACURL != "" {
		return nil, fmt.Errorf("连接 GitHub 失败；检测到 Windows PAC 自动代理，请在网络设置中改用手动代理: %w", err)
	}
	return response, err
}

func updatesSupported() bool { return true }

// CheckForUpdate 查询官方仓库最新稳定版本。后台检查只返回状态，不下载或替换程序。
func (s *DesktopService) CheckForUpdate() (UpdateInfo, error) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateRestarting {
		return UpdateInfo{Supported: true, CurrentVersion: applicationVersion}, errors.New("程序正在重启以完成更新")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	return checkForUpdate(ctx)
}

// InstallUpdate 下载并校验 CheckForUpdate 已确认的 EXE。重启必须等本次 RPC 返回后单独触发。
func (s *DesktopService) InstallUpdate() error {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateRestarting {
		return errors.New("程序正在重启以完成更新")
	}

	app := application.Get()
	if app == nil || app.Updater == nil {
		return errors.New("Windows 更新服务尚未初始化")
	}
	if app.Updater.State() != updater.StateAvailable {
		return errors.New("更新信息已失效，请重新检查更新")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := app.Updater.DownloadAndInstall(ctx); err != nil {
		return fmt.Errorf("下载并校验更新失败: %w", err)
	}
	return nil
}

// RestartUpdate 在 RPC 返回后异步启动 helper，避免 Wails Quit 等待当前 RPC 而阻塞退出。
func (s *DesktopService) RestartUpdate() error {
	s.updateMu.Lock()
	app := application.Get()
	if app == nil || app.Updater == nil {
		s.updateMu.Unlock()
		return errors.New("Windows 更新服务尚未初始化")
	}
	if app.Updater.State() != updater.StateReady {
		s.updateMu.Unlock()
		return errors.New("更新文件尚未下载并校验完成")
	}
	if s.updateRestarting {
		s.updateMu.Unlock()
		return errors.New("程序正在重启以完成更新")
	}
	s.updateRestarting = true
	s.updateMu.Unlock()

	go s.restartUpdate(app)
	return nil
}

func (s *DesktopService) restartUpdate(app *application.App) {
	time.Sleep(updateRestartDelay)

	// Wails helper 只等待父进程 30 秒；正常退出卡住时需留出替换和重启余量。
	forceExit := time.AfterFunc(updateForceExitDelay, func() {
		os.Exit(0)
	})
	if err := app.Updater.Restart(context.Background()); err != nil {
		forceExit.Stop()
		s.failUpdateRestart(app, fmt.Sprintf("启动更新替换程序失败: %v", err))
	}
}

func (s *DesktopService) failUpdateRestart(app *application.App, message string) {
	s.updateMu.Lock()
	if !s.updateRestarting {
		s.updateMu.Unlock()
		return
	}
	s.updateRestarting = false
	s.updateMu.Unlock()
	app.Event.Emit(updateRestartEvent, message)
}

func checkForUpdate(ctx context.Context) (UpdateInfo, error) {
	info := UpdateInfo{Supported: true, CurrentVersion: applicationVersion}
	app := application.Get()
	if app == nil || app.Updater == nil {
		return info, errors.New("Windows 更新服务尚未初始化")
	}
	release, err := app.Updater.Check(ctx)
	if err != nil {
		return info, fmt.Errorf("检查 GitHub 最新版本失败: %w", err)
	}
	if release == nil {
		return info, nil
	}
	if err := validateWindowsUpdate(release); err != nil {
		return info, err
	}
	info.Available = true
	info.LatestVersion = release.Version
	info.Name = release.Name
	info.Size = release.Artifact.Size
	if !release.PublishedAt.IsZero() {
		info.PublishedAt = release.PublishedAt.Format(time.RFC3339)
	}
	return info, nil
}

func validateWindowsUpdate(release *updater.Release) error {
	if release.Artifact.Platform != "windows" {
		return errors.New("最新版本没有匹配的 Windows 更新文件")
	}
	if release.Verification == nil || release.Verification.DigestAlgo != "sha256" || len(release.Verification.Digest) != sha256.Size {
		return fmt.Errorf("版本 %s 缺少有效的 SHA-256 校验信息，已拒绝更新", release.Version)
	}
	return nil
}
