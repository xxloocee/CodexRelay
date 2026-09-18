package clientconfig

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Refuse mutation if process enumeration fails; this is deliberately global,
// since the desktop app and CLI may use different Codex homes.
func ensureCodexHistoryProcessesStopped() error {
	names, err := codexHistoryProcessNames()
	if err != nil {
		return fmt.Errorf("无法确认 Codex 已退出，已停止历史修复: %w", err)
	}
	if len(names) == 0 {
		return errors.New("进程列表为空，已停止历史修复")
	}
	for _, name := range names {
		name = strings.ToLower(filepath.Base(strings.TrimSpace(name)))
		if name == "codex" || name == "codex.exe" || name == "codex-app-server" || name == "codex-app-server.exe" || name == "chatgpt" || name == "chatgpt.exe" || strings.HasPrefix(name, "codex helper") || strings.HasPrefix(name, "chatgpt helper") {
			return errors.New("请完全退出 Codex 桌面版、CLI 和 IDE 中的 Codex 进程后再修复或恢复历史")
		}
	}
	return nil
}
