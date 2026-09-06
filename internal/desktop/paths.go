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
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	oldDataDirectory := s.runtime.DataDirectory()
	if filepath.Clean(oldDataDirectory) == directory {
		return nil
	}
	backupSource := filepath.Join(oldDataDirectory, "client-backups")
	backupTarget := filepath.Join(directory, "client-backups")
	if relative, relErr := filepath.Rel(backupSource, directory); relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("目标数据目录不能位于客户端备份目录内")
	}
	copiedClientBackups := false
	if _, targetErr := os.Stat(backupTarget); targetErr == nil {
		return errors.New("目标数据目录已存在客户端备份，拒绝覆盖")
	} else if !errors.Is(targetErr, os.ErrNotExist) {
		return fmt.Errorf("检查目标客户端备份失败: %w", targetErr)
	}
	if info, statErr := os.Stat(backupSource); statErr == nil {
		if !info.IsDir() {
			return errors.New("客户端备份源路径不是目录，拒绝迁移")
		}
		if err := copyDirectory(backupSource, backupTarget); err != nil {
			_ = os.RemoveAll(backupTarget)
			return fmt.Errorf("迁移客户端备份失败: %w", err)
		}
		copiedClientBackups = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("检查客户端备份源失败: %w", statErr)
	}
	migrate := func() (string, error) {
		return s.runtime.MigrateDataDirectory(directory, func() error {
			return config.SaveDataDirectoryPointer(directory)
		})
	}
	oldDirectory := ""
	copiedTaskNotificationState := false
	var err error
	if s.taskNotifier != nil {
		oldDirectory, copiedTaskNotificationState, err = s.taskNotifier.MigrateStateTo(directory, migrate)
	} else {
		oldDirectory, err = migrate()
	}
	if err != nil {
		if copiedClientBackups {
			_ = os.RemoveAll(backupTarget)
		}
		if s.taskNotifier != nil {
			return fmt.Errorf("迁移任务通知状态失败: %w", err)
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
				application.Get().Logger.Warn("旧数据文件清理失败", "file", name, "error", removeErr)
			}
		}
		if removeErr := os.RemoveAll(filepath.Join(oldDirectory, "client-backups")); removeErr != nil {
			application.Get().Logger.Warn("旧客户端备份清理失败", "error", removeErr)
		}
	}
	s.notifyStateChanged()
	return nil
}

func copyDirectory(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, info.Mode().Perm())
	})
}
