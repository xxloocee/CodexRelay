package tasknotify

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codexrelay/internal/storage"
)

func (m *Manager) scanRolloutLocked(directory, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	current, known := m.cursors[path]
	baseline := !known || info.Size() < current.Offset
	if baseline {
		current = &cursor{Path: path}
		m.cursors[path] = current
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(current.Offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(file, 64*1024)
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 && strings.HasSuffix(line, "\n") {
			current.Offset += int64(len(line))
			m.observeLineLocked(directory, current, line, baseline)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

func (m *Manager) observeLineLocked(directory string, current *cursor, line string, baseline bool) {
	var envelope struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &envelope) != nil {
		return
	}
	if envelope.Type == "session_meta" {
		var meta struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(envelope.Payload, &meta) == nil {
			current.ThreadID = strings.TrimSpace(meta.ID)
		}
		return
	}
	if envelope.Type != "event_msg" || current.ThreadID == "" {
		return
	}
	var event struct {
		Type        string          `json:"type"`
		TurnID      string          `json:"turn_id"`
		StartedAt   json.RawMessage `json:"started_at"`
		CompletedAt json.RawMessage `json:"completed_at"`
		Reason      string          `json:"reason"`
	}
	if json.Unmarshal(envelope.Payload, &event) != nil {
		return
	}
	event.Type, event.TurnID = strings.TrimSpace(event.Type), strings.TrimSpace(event.TurnID)
	if event.TurnID == "" || (event.Type != "task_started" && event.Type != "task_complete" && event.Type != "turn_aborted") {
		return
	}
	current.LastLifecycle, current.LastLifecycleTurn = event.Type, event.TurnID
	if current.TurnStartedAt == nil {
		current.TurnStartedAt = make(map[string]string)
	}
	startedAt := parseRolloutUnixTime(event.StartedAt)
	if startedAt.IsZero() {
		startedAt = parseRolloutTimestamp(envelope.Timestamp)
	}
	if event.Type == "task_started" {
		if !startedAt.IsZero() {
			current.TurnStartedAt[event.TurnID] = formatStoredTime(startedAt)
		}
		return
	}
	if !baseline && (event.Type == "task_complete" || event.Type == "turn_aborted") {
		if startedAt.IsZero() {
			startedAt = parseEventTime(current.TurnStartedAt[event.TurnID])
		}
		occurredAt := parseRolloutUnixTime(event.CompletedAt)
		if occurredAt.IsZero() {
			occurredAt = parseRolloutTimestamp(envelope.Timestamp)
		}
		if occurredAt.IsZero() {
			occurredAt = time.Now().UTC()
		}
		delete(current.TurnStartedAt, event.TurnID)
		details := EventDetails{StartedAt: startedAt, OccurredAt: occurredAt, AbortReason: strings.TrimSpace(event.Reason)}
		m.enqueueCandidateLocked(directory, current, rolloutEventType(event.Type), event.TurnID, details)
	}
}

func (m *Manager) enqueueCandidateLocked(directory string, current *cursor, eventType, turnID string, details EventDetails) {
	if !eventEnabled(m.currentSettings().Events, eventType) {
		return
	}
	key := eventKey(current.ThreadID, turnID)
	name := key + ".json"
	for _, bucket := range []string{"pending", "outbox", "sent", "suppressed", "dead"} {
		if _, err := os.Stat(filepath.Join(directory, bucket, name)); err == nil {
			return
		}
	}
	entry := record{Schema: 1, Key: key, ThreadID: current.ThreadID, TurnID: turnID, EventType: eventType, RolloutPath: current.Path, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	applyEventDetails(&entry, details)
	if err := storage.WriteJSONAtomic(filepath.Join(directory, "pending", name), ".pending-*.tmp", entry); err != nil {
		m.setErrorLocked("创建任务通知候选失败")
	}
}

func (m *Manager) promotePendingLocked(directory string, settings Settings) {
	grace := time.Duration(settings.IdleGraceSeconds) * time.Second
	if grace <= 0 {
		grace = 5 * time.Second
	}
	stateReader, stateErr := m.codexStateReader()
	for _, path := range jsonFiles(filepath.Join(directory, "pending")) {
		entry, err := readRecord(path)
		if err != nil {
			m.deadLocked(directory, filepath.Base(path), "候选记录格式无效")
			_ = os.Remove(path)
			continue
		}
		if !eventEnabled(settings.Events, entry.EventType) {
			m.suppressLocked(directory, path, entry, "通知事件已关闭")
			continue
		}
		if stateErr == nil {
			if stateReader.classifyThread(entry.ThreadID) == threadSubagent {
				m.suppressLocked(directory, path, entry, "子代理任务不单独推送")
				continue
			}
			if available, status := stateReader.goalStatus(entry.ThreadID); available && status == "active" {
				continue
			}
			if available, active := stateReader.activeDescendants(entry.ThreadID, time.Now()); available && active > 0 {
				continue
			}
		}
		current := m.cursors[entry.RolloutPath]
		info, statErr := os.Stat(entry.RolloutPath)
		if current == nil || statErr != nil || current.ThreadID != entry.ThreadID || current.LastLifecycleTurn != entry.TurnID || (current.LastLifecycle != "task_complete" && current.LastLifecycle != "turn_aborted") {
			m.suppressLocked(directory, path, entry, "rollout 已出现更晚生命周期事件")
			continue
		}
		if time.Since(info.ModTime()) < grace {
			continue
		}
		if err := os.Rename(path, filepath.Join(directory, "outbox", filepath.Base(path))); err != nil {
			m.setErrorLocked("确认任务通知候选失败")
		}
	}
}

func (m *Manager) codexStateReader() (codexStateReader, error) {
	if m.sessionsDirectory == nil {
		return codexStateReader{}, errors.New("Codex sessions 目录不可用")
	}
	sessions, err := m.sessionsDirectory()
	if err != nil || strings.TrimSpace(sessions) == "" {
		if err == nil {
			err = errors.New("Codex sessions 目录为空")
		}
		return codexStateReader{}, err
	}
	return newCodexStateReader(sessions), nil
}
