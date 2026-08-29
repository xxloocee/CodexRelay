package tasknotify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"codexrelay/internal/storage"
)

func (m *Manager) deliverOutboxLocked(directory string, settings Settings) {
	stateReader, _ := m.codexStateReader()
	for _, path := range jsonFiles(filepath.Join(directory, "outbox")) {
		entry, err := readRecord(path)
		if err != nil {
			m.deadLocked(directory, filepath.Base(path), "待投递记录格式无效")
			_ = os.Remove(path)
			continue
		}
		if !eventEnabled(settings.Events, entry.EventType) {
			m.suppressLocked(directory, path, entry, "通知事件已关闭")
			continue
		}
		if entry.NextAttempt != "" {
			if next, parseErr := time.Parse(time.RFC3339Nano, entry.NextAttempt); parseErr == nil && time.Now().Before(next) {
				continue
			}
		}
		if stateReader.codexHome != "" && (entry.EventType == EventTaskCompleted || entry.EventType == EventTaskAborted) {
			info := stateReader.displayInfo(entry.ThreadID)
			if info.TaskName != "" {
				entry.TaskName = info.TaskName
			}
			if info.ProjectName != "" {
				entry.ProjectName = info.ProjectName
			}
			// 发送前刷新展示元数据，兼容旧版本已经写入的 pending/outbox 记录。
			_ = storage.WriteJSONAtomic(path, ".outbox-meta-*.tmp", entry)
		}
		title, content := messageForRecord(entry)
		err = postEvent(context.Background(), settings, title, content)
		if err == nil {
			entry.LastError, entry.NextAttempt = "", ""
			if storage.WriteJSONAtomic(filepath.Join(directory, "sent", filepath.Base(path)), ".sent-*.tmp", entry) == nil {
				_ = os.Remove(path)
			}
			continue
		}
		entry.Attempts++
		entry.LastError = safeDeliveryError(err)
		if isPermanentDeliveryError(err) || (settings.MaxAttempts > 0 && entry.Attempts >= settings.MaxAttempts) {
			m.deadLocked(directory, filepath.Base(path), entry.LastError)
			_ = os.Remove(path)
			continue
		}
		entry.NextAttempt = time.Now().Add(retryDelay(entry.Attempts)).UTC().Format(time.RFC3339Nano)
		if storage.WriteJSONAtomic(path, ".outbox-*.tmp", entry) != nil {
			m.setErrorLocked("保存任务通知重试状态失败")
		}
	}
}

func (m *Manager) suppressLocked(directory, pendingPath string, entry record, reason string) {
	receipt := struct {
		Schema int    `json:"schema"`
		Key    string `json:"key"`
		Reason string `json:"reason"`
	}{Schema: 1, Key: entry.Key, Reason: reason}
	if storage.WriteJSONAtomic(filepath.Join(directory, "suppressed", filepath.Base(pendingPath)), ".suppressed-*.tmp", receipt) == nil {
		_ = os.Remove(pendingPath)
	}
}

func (m *Manager) deadLocked(directory, filename, reason string) {
	receipt := struct {
		Schema int    `json:"schema"`
		Reason string `json:"reason"`
	}{Schema: 1, Reason: reason}
	_ = storage.WriteJSONAtomic(filepath.Join(directory, "dead", filename), ".dead-*.tmp", receipt)
}

func (m *Manager) ensureStateLocked() (string, error) {
	directory := m.stateDirectory()
	if directory == "" {
		return "", errors.New("数据目录为空")
	}
	if m.loadedDirectory == directory {
		return directory, nil
	}
	for _, name := range []string{"pending", "outbox", "sent", "suppressed", "dead", "watch"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
			return "", err
		}
	}
	m.cursors = make(map[string]*cursor)
	var stored cursorStore
	exists, err := storage.ReadJSON(filepath.Join(directory, "watch", "cursors.json"), &stored)
	if err != nil {
		return "", err
	}
	if exists {
		if stored.Schema != 1 || stored.Cursors == nil {
			return "", errors.New("任务通知扫描状态格式无效")
		}
		m.cursors = stored.Cursors
	}
	m.loadedDirectory = directory
	return directory, nil
}

func (m *Manager) saveCursorsLocked(directory string) error {
	return storage.WriteJSONAtomic(filepath.Join(directory, "watch", "cursors.json"), ".cursors-*.tmp", cursorStore{Schema: 1, Cursors: m.cursors})
}
