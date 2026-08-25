/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project        : CodexRelay
 * @Description    : Codex API 中转热切换桌面工具
 * @File           : 本机 Codex 任务完成通知 watcher 与耐久投递队列
 * @Read me        : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind         : 二次开发请保留原版权信息，谢谢。
 */
package tasknotify

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codexrelay/internal/storage"
)

const (
	stateDirectoryName = "task-notifications"
	pollInterval       = 2 * time.Second

	// EventTaskCompleted 对应已完成且通过静默确认的本机 rollout 回合。
	EventTaskCompleted = "task_completed"
	// EventTaskAborted 对应已中止且通过静默确认的本机 rollout 回合。
	EventTaskAborted = "task_aborted"
	// EventTokenRequestFailed 对应令牌请求达到现有健康统计阈值的故障轮次。
	EventTokenRequestFailed = "token_request_failed"
	// EventTokenAutoSwitched 对应本次运行中已经成功提交的自动令牌切换。
	EventTokenAutoSwitched = "token_auto_switched"
	// EventTokenAutoSwitchFailed 对应自动切换候选耗尽后的停止状态。
	EventTokenAutoSwitchFailed = "token_auto_switch_failed"
	// EventAccountBalanceLow 对应二狗子账户首次进入低余额提醒状态。
	EventAccountBalanceLow = "account_balance_low"
	// EventSubscriptionBalanceLow 对应二狗子套餐首次进入低余额提醒状态。
	EventSubscriptionBalanceLow = "subscription_balance_low"
)

// EventSettings 是桌面配置层提供的事件选择快照。任务终态需要先经过 rollout
// 静默确认；其他来源在其本地状态变化已经确认后直接写入 outbox。
type EventSettings struct {
	TaskCompleted          bool
	TaskAborted            bool
	TokenRequestFailed     bool
	TokenAutoSwitched      bool
	TokenAutoSwitchFailed  bool
	AccountBalanceLow      bool
	SubscriptionBalanceLow bool
}

// Settings 是桌面配置层提供给通知模块的最小运行时快照。WebhookURL 是用户完整填写的
// 访问地址，其中可使用 {title}、{content}；投递前只替换这两个占位符并进行 URL 编码。
type Settings struct {
	Enabled               bool
	WebhookURL            string
	Events                EventSettings
	IdleGraceSeconds      int
	RequestTimeoutSeconds int
	MaxAttempts           int
}

// EventDetails 是已确认本地事件允许写入耐久队列的展示信息。字段不包含 API 密钥、
// prompt、路径或上游原始错误，重试始终复用同一份记录，避免同一事件收到不同正文。
type EventDetails struct {
	StartedAt            time.Time
	OccurredAt           time.Time
	Category             string
	FromGroup            string
	ToGroup              string
	AmountUSD            float64
	ThresholdUSD         float64
	FailureKind          string
	FailureCount         int
	FailureStatus        int
	FailureWindowMinutes int
	AbortReason          string
}

// Status 仅公开队列计数和非敏感错误摘要，供首页和设置页显示。
type Status struct {
	Enabled   bool   `json:"enabled"`
	Pending   int    `json:"pending"`
	Outbox    int    `json:"outbox"`
	Dead      int    `json:"dead"`
	LastError string `json:"lastError,omitempty"`
}

type cursor struct {
	Path              string            `json:"path"`
	Offset            int64             `json:"offset"`
	ThreadID          string            `json:"threadId"`
	LastLifecycle     string            `json:"lastLifecycle,omitempty"`
	LastLifecycleTurn string            `json:"lastLifecycleTurn,omitempty"`
	TurnStartedAt     map[string]string `json:"turnStartedAt,omitempty"`
}

type cursorStore struct {
	Schema  int                `json:"schema"`
	Cursors map[string]*cursor `json:"cursors"`
}

type record struct {
	Schema               int     `json:"schema"`
	Key                  string  `json:"key"`
	ThreadID             string  `json:"threadId"`
	TurnID               string  `json:"turnId"`
	EventType            string  `json:"eventType"`
	EventID              string  `json:"eventId,omitempty"`
	RolloutPath          string  `json:"rolloutPath"`
	CreatedAt            string  `json:"createdAt"`
	StartedAt            string  `json:"startedAt,omitempty"`
	OccurredAt           string  `json:"occurredAt,omitempty"`
	TaskName             string  `json:"taskName,omitempty"`
	ProjectName          string  `json:"projectName,omitempty"`
	AbortReason          string  `json:"abortReason,omitempty"`
	Category             string  `json:"category,omitempty"`
	FromGroup            string  `json:"fromGroup,omitempty"`
	ToGroup              string  `json:"toGroup,omitempty"`
	AmountUSD            float64 `json:"amountUsd,omitempty"`
	ThresholdUSD         float64 `json:"thresholdUsd,omitempty"`
	FailureKind          string  `json:"failureKind,omitempty"`
	FailureCount         int     `json:"failureCount,omitempty"`
	FailureStatus        int     `json:"failureStatus,omitempty"`
	FailureWindowMinutes int     `json:"failureWindowMinutes,omitempty"`
	Attempts             int     `json:"attempts,omitempty"`
	NextAttempt          string  `json:"nextAttempt,omitempty"`
	LastError            string  `json:"lastError,omitempty"`
}

// Manager 只读取当前用户可见的 rollout JSONL。首次遇到文件只建立扫描光标，
// 不能补发旧任务；所有投递状态均保存在 CodexRelay 的便携数据目录。
type Manager struct {
	settings          func() Settings
	dataDirectory     func() string
	sessionsDirectory func() (string, error)
	onChanged         func()

	mu              sync.Mutex
	loadedDirectory string
	cursors         map[string]*cursor
	lastError       string
}

// NewManager 创建独立任务通知服务。配置和数据目录均按调用时读取，保存设置或
// 切换数据目录不需要重启透明代理。
func NewManager(settings func() Settings, dataDirectory func() string, onChanged func()) *Manager {
	return &Manager{settings: settings, dataDirectory: dataDirectory, sessionsDirectory: defaultSessionsDirectory, onChanged: onChanged, cursors: make(map[string]*cursor)}
}

// Start 在后台扫描 .codex/sessions；调用方取消 ctx 后停止，未完成 outbox 保留到下次启动。
func (m *Manager) Start(ctx context.Context) {
	go func() {
		m.runOnce()
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.runOnce()
			}
		}
	}()
}

// Status 返回本机耐久队列的摘要；目录失败时不伪装为健康状态。
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings := m.currentSettings()
	directory, err := m.ensureStateLocked()
	status := Status{Enabled: settings.Enabled, LastError: m.lastError}
	if err != nil {
		status.LastError = "任务通知状态目录不可用"
		return status
	}
	status.Pending = countJSONFiles(filepath.Join(directory, "pending"))
	status.Outbox = countJSONFiles(filepath.Join(directory, "outbox"))
	status.Dead = countJSONFiles(filepath.Join(directory, "dead"))
	return status
}

// Test 仅发送固定测试事件，不读取 rollout 或修改 pending/outbox。
func (m *Manager) Test(ctx context.Context) error {
	settings := m.currentSettings()
	if strings.TrimSpace(settings.WebhookURL) == "" {
		return errors.New("请先填写任务通知 Webhook 地址")
	}
	return postEvent(ctx, settings, "CodexRelay 消息通知测试", "这是一条 CodexRelay 消息通知测试，发送时间："+formatEventTime(time.Now()))
}

// Enqueue 把已经由本地组件确认的非 rollout 事件写入 outbox。identity 仅用于本地
// 去重，不能包含 API 密钥、任务内容或任何会随请求发出的数据。details 会和事件一起
// 耐久保存，后续重试不重新读取运行时状态或生成不同的通知正文。
func (m *Manager) Enqueue(eventType, identity string, detailValues ...EventDetails) error {
	eventType, identity = strings.TrimSpace(eventType), strings.TrimSpace(identity)
	if !knownEventType(eventType) || identity == "" {
		return errors.New("通知事件无效")
	}
	settings := m.currentSettings()
	if !settings.Enabled || !eventEnabled(settings.Events, eventType) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	directory, err := m.ensureStateLocked()
	if err != nil {
		m.setErrorLocked("任务通知状态目录不可用")
		m.changed()
		return err
	}
	key := externalEventKey(eventType, identity)
	name := key + ".json"
	for _, bucket := range []string{"outbox", "sent", "dead"} {
		if _, statErr := os.Stat(filepath.Join(directory, bucket, name)); statErr == nil {
			return nil
		}
	}
	entry := record{Schema: 1, Key: key, EventType: eventType, EventID: identity, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if len(detailValues) > 0 {
		applyEventDetails(&entry, detailValues[0])
	}
	if err := storage.WriteJSONAtomic(filepath.Join(directory, "outbox", name), ".outbox-*.tmp", entry); err != nil {
		m.setErrorLocked("创建通知待投递记录失败")
		m.changed()
		return err
	}
	m.changed()
	return nil
}

// CopyStateTo 在数据目录提交前复制本模块私有状态。目标目录已有状态时拒绝覆盖，
// 避免未投递事件和已发送收据被错误合并。
func (m *Manager) CopyStateTo(targetDataDirectory string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	source := m.stateDirectory()
	target := filepath.Join(filepath.Clean(targetDataDirectory), stateDirectoryName)
	if _, err := os.Stat(target); err == nil {
		return false, errors.New("目标数据目录已存在任务通知状态，拒绝覆盖")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("检查目标任务通知状态失败: %w", err)
	}
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("检查任务通知状态失败: %w", err)
	}
	return true, copyDirectory(source, target)
}

// DiscardCopiedState 只删除本次迁移预复制出的状态，调用方只能在主迁移失败时调用。
func (m *Manager) DiscardCopiedState(targetDataDirectory string) {
	_ = os.RemoveAll(filepath.Join(filepath.Clean(targetDataDirectory), stateDirectoryName))
}

// FinalizeMigration 在主配置已切换后删除旧状态；删除失败只保留重复本地状态，不影响新目录继续工作。
func (m *Manager) FinalizeMigration(oldDataDirectory string, copied bool) error {
	if !copied {
		return nil
	}
	return os.RemoveAll(filepath.Join(filepath.Clean(oldDataDirectory), stateDirectoryName))
}

func (m *Manager) runOnce() {
	m.mu.Lock()
	defer m.mu.Unlock()
	directory, err := m.ensureStateLocked()
	if err != nil {
		m.setErrorLocked("任务通知状态目录不可用")
		m.changed()
		return
	}
	settings := m.currentSettings()
	if !settings.Enabled {
		m.lastError = ""
		m.changed()
		return
	}
	rollouts, err := m.findRolloutsLocked()
	if err != nil {
		m.setErrorLocked("无法读取 Codex rollout 目录")
		m.changed()
		return
	}
	for _, path := range rollouts {
		if err := m.scanRolloutLocked(directory, path); err != nil {
			m.setErrorLocked("读取 Codex rollout 失败")
		}
	}
	if err := m.saveCursorsLocked(directory); err != nil {
		m.setErrorLocked("保存任务通知扫描位置失败")
	}
	m.promotePendingLocked(directory, settings)
	m.deliverOutboxLocked(directory, settings)
	m.changed()
}

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

func (m *Manager) currentSettings() Settings {
	if m.settings == nil {
		return Settings{}
	}
	return m.settings()
}

func (m *Manager) stateDirectory() string {
	if m.dataDirectory == nil {
		return ""
	}
	base := strings.TrimSpace(m.dataDirectory())
	if base == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(base), stateDirectoryName)
}

func (m *Manager) setErrorLocked(message string) { m.lastError = message }
func (m *Manager) changed() {
	if m.onChanged != nil {
		go m.onChanged()
	}
}

func (m *Manager) findRolloutsLocked() ([]string, error) {
	root, err := m.sessionsDirectory()
	if err != nil {
		return nil, err
	}
	return findRollouts(root)
}

func defaultSessionsDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

func findRollouts(root string) ([]string, error) {
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	} else if err != nil {
		return nil, err
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "rollout-") && strings.HasSuffix(entry.Name(), ".jsonl") {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func eventKey(threadID, turnID string) string {
	digest := sha256.Sum256([]byte("codexrelay/task-notification/v1\x00" + threadID + "\x00" + turnID))
	return hex.EncodeToString(digest[:])
}

func externalEventKey(eventType, identity string) string {
	digest := sha256.Sum256([]byte("codexrelay/notification-event/v1\x00" + eventType + "\x00" + identity))
	return hex.EncodeToString(digest[:])
}

func rolloutEventType(lifecycle string) string {
	switch lifecycle {
	case "task_complete":
		return EventTaskCompleted
	case "turn_aborted":
		return EventTaskAborted
	default:
		return ""
	}
}

func knownEventType(eventType string) bool {
	switch eventType {
	case EventTaskCompleted, EventTaskAborted, EventTokenRequestFailed, EventTokenAutoSwitched, EventTokenAutoSwitchFailed, EventAccountBalanceLow, EventSubscriptionBalanceLow:
		return true
	default:
		return false
	}
}

func eventEnabled(events EventSettings, eventType string) bool {
	switch eventType {
	case EventTaskCompleted:
		return events.TaskCompleted
	case EventTaskAborted:
		return events.TaskAborted
	case EventTokenRequestFailed:
		return events.TokenRequestFailed
	case EventTokenAutoSwitched:
		return events.TokenAutoSwitched
	case EventTokenAutoSwitchFailed:
		return events.TokenAutoSwitchFailed
	case EventAccountBalanceLow:
		return events.AccountBalanceLow
	case EventSubscriptionBalanceLow:
		return events.SubscriptionBalanceLow
	default:
		return false
	}
}

func readRecord(path string) (record, error) {
	var entry record
	exists, err := storage.ReadJSON(path, &entry)
	if err != nil || !exists || entry.Schema != 1 || entry.Key == "" || !knownEventType(entry.EventType) {
		return record{}, errors.New("记录格式无效")
	}
	if entry.EventID == "" && (entry.TurnID == "" || entry.RolloutPath == "") {
		return record{}, errors.New("记录格式无效")
	}
	return entry, nil
}

func jsonFiles(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}
func countJSONFiles(directory string) int { return len(jsonFiles(directory)) }

type deliveryError struct {
	status int
	err    error
}

func (e *deliveryError) Error() string {
	if e.status > 0 {
		return fmt.Sprintf("通知服务返回 HTTP %d", e.status)
	}
	return "通知请求失败"
}

// postEvent 只替换 URL 中字面量 {title}、{content}，并将两者按查询参数编码；不会
// 追加参数、识别第三方协议或写入请求体。没有占位符的 URL 仍按用户填写内容直接访问。

func postEvent(parent context.Context, settings Settings, values ...string) error {
	title, content := "", ""
	if len(values) > 0 {
		title = values[0]
	}
	if len(values) > 1 {
		content = values[1]
	}
	endpointURL := strings.ReplaceAll(settings.WebhookURL, "{title}", escapeURLComponent(title))
	endpointURL = strings.ReplaceAll(endpointURL, "{content}", escapeURLComponent(content))
	endpoint, err := url.Parse(endpointURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		return errors.New("任务通知 URL 无效")
	}
	timeout := time.Duration(settings.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return &deliveryError{err: err}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &deliveryError{status: response.StatusCode}
	}
	return nil
}

// escapeURLComponent 使用查询参数的保留字符集合，并把消息中的普通空格编码为
// RFC 3986 要求的 %20；消息中的字面加号则编码为 %2B，避免被服务端误认为是空格。
func escapeURLComponent(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func applyEventDetails(entry *record, details EventDetails) {
	if entry == nil {
		return
	}
	entry.StartedAt = formatStoredTime(details.StartedAt)
	entry.OccurredAt = formatStoredTime(details.OccurredAt)
	entry.Category = strings.TrimSpace(details.Category)
	entry.FromGroup = strings.TrimSpace(details.FromGroup)
	entry.ToGroup = strings.TrimSpace(details.ToGroup)
	entry.AmountUSD = details.AmountUSD
	entry.ThresholdUSD = details.ThresholdUSD
	entry.FailureKind = strings.TrimSpace(details.FailureKind)
	entry.FailureCount = details.FailureCount
	entry.FailureStatus = details.FailureStatus
	entry.FailureWindowMinutes = details.FailureWindowMinutes
	entry.AbortReason = strings.TrimSpace(details.AbortReason)
}

func formatStoredTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseEventTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// parseRolloutUnixTime 解析 rollout 生命周期字段中的 Unix 秒。该格式来自本机
// rollout JSONL 的 task_started、task_complete 和 turn_aborted 事件；字段缺失或
// 格式无法确认时返回零值，由调用方使用已保存的同一回合时间或事件时间兜底。
func parseRolloutUnixTime(value json.RawMessage) time.Time {
	text := strings.TrimSpace(string(value))
	if text == "" || text == "null" {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

// parseRolloutTimestamp 解析 rollout 顶层事件时间，用于生命周期载荷未提供数值
// 时间时保留同一条事件的真实写入时间。
func parseRolloutTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func formatEventTime(value time.Time) string {
	if value.IsZero() {
		return "时间未记录"
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func formatDuration(started, occurred time.Time) string {
	if started.IsZero() || occurred.IsZero() || occurred.Before(started) {
		return "开始时间未记录"
	}
	seconds := int64(occurred.Sub(started) / time.Second)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	seconds %= 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%d小时%02d分%02d秒", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%d分%02d秒", minutes, seconds)
	default:
		return fmt.Sprintf("%d秒", seconds)
	}
}

func displayGroup(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未分组"
	}
	return value
}

func messageForRecord(entry record) (string, string) {
	occurred := parseEventTime(entry.OccurredAt)
	if occurred.IsZero() {
		occurred = parseEventTime(entry.CreatedAt)
	}
	when := formatEventTime(occurred)
	switch entry.EventType {
	case EventTaskCompleted:
		return taskNotificationTitle(entry, "任务已完成"), fmt.Sprintf("当前任务：【%s】项目的【%s】已完成，完成耗时：%s，完成时间：%s", displayProject(entry), displayTask(entry), formatDuration(parseEventTime(entry.StartedAt), occurred), when)
	case EventTaskAborted:
		return taskNotificationTitle(entry, "任务异常中断"), fmt.Sprintf("当前任务：【%s】项目的【%s】异常中断，原因：%s，已运行：%s，中断时间：%s", displayProject(entry), displayTask(entry), displayAbortReason(entry), formatDuration(parseEventTime(entry.StartedAt), occurred), when)
	case EventTokenRequestFailed:
		return "令牌请求故障", tokenRequestFailureMessage(entry, when)
	case EventTokenAutoSwitched:
		return "令牌已自动切换", fmt.Sprintf("当前类别：%s，已从分组：%s 切换到分组：%s，切换时间：%s", displayGroup(entry.Category), displayGroup(entry.FromGroup), displayGroup(entry.ToGroup), when)
	case EventTokenAutoSwitchFailed:
		return "令牌自动切换失败", fmt.Sprintf("当前类别：%s，尝试从分组：%s 切换到分组：%s，切换结果：没有可用的备用令牌，发生时间：%s", displayGroup(entry.Category), displayGroup(entry.FromGroup), displayGroup(entry.ToGroup), when)
	case EventAccountBalanceLow:
		return "账户余额不足", fmt.Sprintf("账户余额不足，当前余额：$%.2f，提醒阈值：$%.2f，检测时间：%s", entry.AmountUSD, entry.ThresholdUSD, when)
	case EventSubscriptionBalanceLow:
		return "套餐余额不足", fmt.Sprintf("套餐余额不足，当前余额：$%.2f，提醒阈值：$%.2f，检测时间：%s", entry.AmountUSD, entry.ThresholdUSD, when)
	default:
		return "CodexRelay 消息通知", fmt.Sprintf("发生时间：%s", when)
	}
}

func displayProject(entry record) string {
	if value := strings.TrimSpace(entry.ProjectName); value != "" {
		return value
	}
	return "未归类"
}

func displayTask(entry record) string {
	if value := strings.TrimSpace(entry.TaskName); value != "" {
		return value
	}
	return "任务名称未记录"
}

func displayAbortReason(entry record) string {
	if value := strings.TrimSpace(entry.AbortReason); value != "" {
		return value
	}
	return "未记录"
}

func taskNotificationTitle(entry record, suffix string) string {
	if project := strings.TrimSpace(entry.ProjectName); project != "" {
		return fmt.Sprintf("【%s】%s", project, suffix)
	}
	return suffix
}

func tokenRequestFailureMessage(entry record, when string) string {
	kind := "上游异常"
	switch entry.FailureKind {
	case "auth":
		kind = fmt.Sprintf("连续 %d 次返回 HTTP %d", entry.FailureCount, entry.FailureStatus)
	case "upstream":
		status := "5xx 或网络故障"
		if entry.FailureStatus >= 500 {
			status = fmt.Sprintf("最近返回 HTTP %d", entry.FailureStatus)
		}
		kind = fmt.Sprintf("%d 分钟内累计 %d 次%s", entry.FailureWindowMinutes, entry.FailureCount, status)
	}
	return fmt.Sprintf("当前类别：%s，令牌请求达到故障阈值：%s，发生时间：%s", displayGroup(entry.Category), kind, when)
}

func isPermanentDeliveryError(err error) bool {
	var delivery *deliveryError
	return errors.As(err, &delivery) && delivery.status > 0 && ((delivery.status >= 300 && delivery.status < 400) || (delivery.status >= 400 && delivery.status < 500 && delivery.status != http.StatusRequestTimeout && delivery.status != http.StatusTooManyRequests))
}
func safeDeliveryError(err error) string {
	var delivery *deliveryError
	if errors.As(err, &delivery) {
		return delivery.Error()
	}
	return "通知请求失败"
}
func retryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 10 {
		attempts = 10
	}
	delay := time.Second * time.Duration(1<<(attempts-1))
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}

func copyDirectory(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return storage.WriteBytesAtomic(destination, ".copy-*.tmp", data, 0o600)
	})
}
