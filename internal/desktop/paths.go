/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : CodexRelay 数据目录迁移与原生目录选择
 * @File          : 数据目录和路径选择桌面接口
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codexrelay/internal/config"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SelectDirectory 打开 Wails 原生目录选择器；取消选择返回空字符串且不报错。
func (s *DesktopService) SelectDirectory(initialDirectory string) (string, error) {
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("选择目录").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true).
		ShowHiddenFiles(true)
	initialDirectory = strings.TrimSpace(initialDirectory)
	if initialDirectory != "" {
		if info, err := os.Stat(initialDirectory); err == nil && info.IsDir() {
			dialog.SetDirectory(initialDirectory)
		}
	}
	selected, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("选择目录失败: %w", err)
	}
	return normalizeSelectedDirectory(selected), nil
}

// normalizeSelectedDirectory 保留目录选择器的取消语义；空选择不能被 filepath.Clean 误变成当前目录。
func normalizeSelectedDirectory(selected string) string {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return ""
	}
	return filepath.Clean(selected)
}

// SetDataDirectory 迁移 config.json、usage.json 和任务通知私有队列，并让当前进程后续读写使用新目录。
// 目标同名文件或任务通知状态不会覆盖；主配置失败会删除本次预复制的通知状态。
func (s *DesktopService) SetDataDirectory(directory string) error {
	directory = filepath.Clean(strings.TrimSpace(directory))
	if directory == "" || !filepath.IsAbs(directory) {
		return errors.New("CodexRelay 数据目录必须是绝对路径")
	}
	oldDirectory := s.runtime.DataDirectory()
	copiedTaskNotificationState := false
	if s.taskNotifier != nil && filepath.Clean(oldDirectory) != directory {
		var err error
		copiedTaskNotificationState, err = s.taskNotifier.CopyStateTo(directory)
		if err != nil {
			return fmt.Errorf("迁移任务通知状态失败: %w", err)
		}
	}
	oldDirectory, err := s.runtime.MigrateDataDirectory(directory, func() error {
		return config.SaveDataDirectoryPointer(directory)
	})
	if err != nil {
		if copiedTaskNotificationState && s.taskNotifier != nil {
			s.taskNotifier.DiscardCopiedState(directory)
		}
		return err
	}
	if s.taskNotifier != nil {
		if err := s.taskNotifier.FinalizeMigration(oldDirectory, copiedTaskNotificationState); err != nil {
			application.Get().Logger.Warn("旧任务通知状态清理失败", "error", err)
		}
	}
	if filepath.Clean(oldDirectory) != directory {
		for _, name := range []string{"config.json", "usage.json"} {
			if removeErr := os.Remove(filepath.Join(oldDirectory, name)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return fmt.Errorf("清理旧数据文件 %s 失败: %w", name, removeErr)
			}
		}
	}
	s.notifyStateChanged()
	return nil
}
