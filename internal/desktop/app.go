/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : Wails 桌面应用与窗口装配
 */
package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	resources "codexrelay"
	"codexrelay/internal/config"
	"codexrelay/internal/platform"
	"codexrelay/internal/relay"
	"codexrelay/internal/usage"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

var singleInstanceKey = [32]byte{
	0x43, 0x6f, 0x64, 0x65, 0x78, 0x52, 0x65, 0x6c,
	0x61, 0x79, 0x2d, 0x57, 0x61, 0x69, 0x6c, 0x73,
	0x2d, 0x76, 0x33, 0x2d, 0x53, 0x69, 0x6e, 0x67,
	0x6c, 0x65, 0x2d, 0x49, 0x6e, 0x73, 0x74, 0x21,
}

type LaunchOptions struct {
	ProxyPort int
	Autostart bool
}

// Run 组装并运行 Wails 应用。Autostart 是唯一允许初始隐藏窗口的启动来源。
func Run(options LaunchOptions) error {
	dataDirectory, err := config.ResolveDataDirectory()
	if err != nil {
		return fmt.Errorf("定位 CodexRelay 数据目录失败: %w", err)
	}
	configPath := filepath.Join(dataDirectory, "config.json")
	usagePath := filepath.Join(dataDirectory, "usage.json")
	needsOnboarding, err := onboardingRequired(configPath, usagePath)
	if err != nil {
		return fmt.Errorf("检查首次启动状态失败: %w", err)
	}
	configStore := config.NewStore(configPath)
	if needsOnboarding {
		configStore = config.NewDeferredStore(configPath)
	}
	cfg, err := configStore.LoadOrCreate(options.ProxyPort)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	if cfg.Preferences.LaunchAtStartup {
		if err := platform.SetLaunchAtStartup(true); err != nil {
			return fmt.Errorf("更新开机启动路径失败: %w", err)
		}
	}
	var usageStore *usage.Store
	if needsOnboarding {
		usageStore, err = usage.NewDeferredStore(usagePath)
	} else {
		usageStore, err = usage.NewStore(usagePath)
	}
	if err != nil {
		return fmt.Errorf("加载用量统计失败: %w", err)
	}
	runtime, err := relay.New(configStore, usageStore, cfg)
	if err != nil {
		return fmt.Errorf("初始化失败: %w", err)
	}
	service := NewDesktopService(runtime)
	service.setNeedsOnboarding(needsOnboarding)
	assets, err := resources.FrontendAssets()
	if err != nil {
		return err
	}
	icon := resources.AppIcon()

	var window *application.WebviewWindow
	var restoreWindow func()
	wailsApp := application.New(application.Options{
		Name:        "CodexRelay",
		Description: "Codex API 中转热切换工具",
		Icon:        icon,
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:      "com.codexrelay.desktop",
			EncryptionKey: singleInstanceKey,
			AdditionalData: map[string]string{
				"launchedAt": time.Now().Format(time.RFC3339),
			},
			OnSecondInstanceLaunch: func(_ application.SecondInstanceData) {
				if restoreWindow != nil {
					restoreWindow()
					return
				}
				if window != nil {
					window.Restore()
					window.Show()
					window.Focus()
				}
			},
		},
	})
	if err := configureUpdater(wailsApp, runtime); err != nil {
		return fmt.Errorf("初始化 Windows 更新服务失败: %w", err)
	}

	windowBackground := application.NewRGB(255, 255, 255)
	if cfg.Preferences.ColorMode == config.ColorModeDark {
		windowBackground = application.NewRGB(12, 16, 21)
	}
	window = wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "CodexRelay",
		Width:            1120,
		Height:           780,
		MinWidth:         760,
		MinHeight:        560,
		URL:              "/",
		Hidden:           shouldStartHidden(options.Autostart, cfg.Preferences),
		BackgroundColour: windowBackground,
	})
	emitDefaultView := func() {
		state := runtime.State()
		if state != nil && state.Config.Preferences.RestoreViewMode == config.RestoreViewDefault {
			window.EmitEvent("relay-restore-default-view")
		}
	}
	restoreWindow = func() {
		emitDefaultView()
		window.Restore()
		window.Show()
		window.Focus()
	}
	// 任务栏最小化和恢复不经过托盘入口；Wails 的窗口状态事件只清理/恢复主页筛选内存状态。
	window.OnWindowEvent(events.Common.WindowMinimise, func(_ *application.WindowEvent) { emitDefaultView() })
	window.OnWindowEvent(events.Common.WindowUnMinimise, func(_ *application.WindowEvent) { emitDefaultView() })
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		// updater.Restart 已经把新文件校验并交给 helper；此时必须真正退出，
		// 否则“关闭到托盘”会拦截 app.Quit，helper 无法完成替换。
		if wailsApp.Updater != nil && wailsApp.Updater.State() == updater.StateReady {
			return
		}
		state := runtime.State()
		if state != nil && state.Config.Preferences.CloseToTray {
			window.Hide()
			event.Cancel()
		}
	})
	notificationManager := newNotificationWindowManager(wailsApp, window, service)
	// 状态写入已经完成后再异步刷新提醒窗口，避免原生窗口隐藏/显示阻塞配置切换接口返回。
	service.addStateChangedHandler(func() { go notificationManager.refresh() })
	setupTray(wailsApp, window, runtime, service, icon, restoreWindow)
	return wailsApp.Run()
}

// onboardingRequired 在配置和用量存储自动创建前检查首次启动条件；任一便携文件缺失就需要引导。
// 非“文件不存在”的文件系统错误必须返回，避免把权限或路径问题误报为首次启动。
func onboardingRequired(configPath, usagePath string) (bool, error) {
	for _, path := range []string{configPath, usagePath} {
		if _, err := os.Stat(path); err == nil {
			continue
		} else if errors.Is(err, os.ErrNotExist) {
			return true, nil
		} else {
			return false, fmt.Errorf("检查 %q 失败: %w", path, err)
		}
	}
	return false, nil
}

func shouldStartHidden(launchedFromAutostart bool, preferences config.Preferences) bool {
	return launchedFromAutostart && preferences.StartHidden
}
