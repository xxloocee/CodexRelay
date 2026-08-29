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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
