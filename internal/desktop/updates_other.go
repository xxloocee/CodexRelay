//go:build !windows

/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 非 Windows 平台关闭应用内更新能力
 * @File          : 非 Windows 桌面更新边界
 */
package desktop

import (
	"errors"

	"codexrelay/internal/relay"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func configureUpdater(_ *application.App, _ *relay.Runtime) error { return nil }

func updatesSupported() bool { return false }

func (s *DesktopService) CheckForUpdate() (UpdateInfo, error) {
	return UpdateInfo{Supported: false, CurrentVersion: applicationVersion}, nil
}

func (s *DesktopService) InstallUpdate() error {
	return errors.New("应用内更新当前仅支持 Windows")
}

func (s *DesktopService) RestartUpdate() error {
	return errors.New("应用内更新当前仅支持 Windows")
}
