/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 前端绑定服务与本地代理生命周期
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/relay"
	"codexrelay/internal/tasknotify"
)

var applicationVersion = "2.3.0"

const (
	NotificationKindBalance      = "balance"
	NotificationKindSubscription = "subscription"
	NotificationKindAnnouncement = "announcement"
	NotificationKindTokenSwitch  = "token-switch"
	dogeProfileURL               = "https://ergouzi.life/profile"
)

type DesktopService struct {
	runtime                  *relay.Runtime
	server                   *http.Server
	listener                 net.Listener
	mu                       sync.Mutex
	onStateChanged           func()
	stateChanged             []func()
	needsOnboarding          bool
	dogeMu                   sync.Mutex
	updateMu                 sync.Mutex
	clientConfigMu           sync.Mutex
	failoverMu               sync.Mutex
	dogeSyncing              bool
	dogeSyncPhase            string
	announcementSyncing      bool
	dogeAlertsSuppressed     bool
	switchMu                 sync.Mutex
	switchPrompts            map[string]*tokenSwitchPromptState
	switchRounds             map[string]*tokenSwitchRound
	directorySwitches        map[string]*tokenSwitchContext
	directoryRecoveryNotices map[string]*PublicDogeTokenSwitchPrompt
	autoSwitchNotices        map[string]*PublicDogeTokenSwitchPrompt
	syncCancel               context.CancelFunc
	syncStarted              bool
	taskNotifier             *tasknotify.Manager
	taskNotifyCancel         context.CancelFunc
	taskNotifyStarted        bool
}

type tokenSwitchPromptState struct {
	dismissed       bool
	suppressedUntil time.Time
}

// tokenSwitchRound 保存一个 API 类别在本次运行中的故障轮次；不写入配置文件。
// 每个 Profile 在一轮中最多被尝试一次，全部候选都失败后停止自动切换，避免循环使用故障令牌。
type tokenSwitchRound struct {
	AttemptedIDs map[string]struct{}
	History      []PublicDogeTokenSwitchHistory
	Stopped      bool
	StoppedAt    time.Time
	StopMessage  string
}

// taskNotificationEvent 保存已确认的本地状态变化及其本地去重标识。该标识只写入
// 耐久队列，不会被追加到用户配置的推送 URL。
type taskNotificationEvent struct {
	Type     string
	Identity string
	Details  tasknotify.EventDetails
}

type tokenSwitchContext struct {
	key                  string
	failureKind          string
	directoryReason      string
	failureCount         int
	failureStatus        int
	failureWindowMinutes int
	health               relay.HealthSnapshot
	profile              config.Profile
	token                config.DogeToken
	candidates           []config.DogeToken
	profilesByID         map[int64]config.Profile
	tokens               []config.DogeToken
	groups               []string
	candidateProfiles    []config.Profile
	failoverOrder        []string
}

const (
	dogeDirectoryFailureMissing     = "missing"
	dogeDirectoryFailureUnavailable = "unavailable"
)

func NewDesktopService(runtime *relay.Runtime) *DesktopService {
	service := &DesktopService{
		runtime: runtime, switchPrompts: make(map[string]*tokenSwitchPromptState), switchRounds: make(map[string]*tokenSwitchRound),
		directorySwitches: make(map[string]*tokenSwitchContext), directoryRecoveryNotices: make(map[string]*PublicDogeTokenSwitchPrompt), autoSwitchNotices: make(map[string]*PublicDogeTokenSwitchPrompt),
	}
	service.taskNotifier = tasknotify.NewManager(func() tasknotify.Settings {
		state := runtime.State()
		if state == nil {
			return tasknotify.Settings{}
		}
		setting := state.Config.TaskNotification
		return tasknotify.Settings{Enabled: setting.Enabled, WebhookURL: setting.WebhookURL, Events: tasknotify.EventSettings{TaskCompleted: setting.Events.TaskCompleted, TaskAborted: setting.Events.TaskAborted, TokenRequestFailed: setting.Events.TokenRequestFailed, TokenAutoSwitched: setting.Events.TokenAutoSwitched, TokenAutoSwitchFailed: setting.Events.TokenAutoSwitchFailed, AccountBalanceLow: setting.Events.AccountBalanceLow, SubscriptionBalanceLow: setting.Events.SubscriptionBalanceLow}, IdleGraceSeconds: setting.IdleGraceSeconds, RequestTimeoutSeconds: setting.RequestTimeoutSeconds, MaxAttempts: setting.MaxAttempts}
	}, runtime.DataDirectory, service.notifyStateChanged)
	runtime.SetHealthChangedHandler(service.handleHealthChanged)
	runtime.SetResultObservedHandler(service.handleUpstreamResult)
	return service
}

func (s *DesktopService) setStateChangedHandler(handler func()) {
	s.mu.Lock()
	s.onStateChanged = handler
	s.mu.Unlock()
}

func (s *DesktopService) addStateChangedHandler(handler func()) {
	if handler == nil {
		return
	}
	s.mu.Lock()
	s.stateChanged = append(s.stateChanged, handler)
	s.mu.Unlock()
}

func (s *DesktopService) notifyStateChanged() {
	s.mu.Lock()
	if s.dogeAlertsSuppressed {
		s.mu.Unlock()
		return
	}
	handler := s.onStateChanged
	handlers := append([]func(){}, s.stateChanged...)
	s.mu.Unlock()
	if handler != nil {
		handler()
	}
	for _, callback := range handlers {
		callback()
	}
}

// setDogeAlertsSuppressed 暂停绑定流程中的状态广播，避免首次绑定同步产生右下角提醒。
// 记录仍正常写入 config.json；标记只存于当前进程，下一次同步或重启后提醒按持久化记录恢复。
func (s *DesktopService) setDogeAlertsSuppressed(value bool) {
	s.mu.Lock()
	s.dogeAlertsSuppressed = value
	s.mu.Unlock()
}

// setNeedsOnboarding 设置本次进程的首次启动引导状态；状态不写入配置，配置与用量文件存在后下次启动自然跳过。
func (s *DesktopService) setNeedsOnboarding(value bool) {
	s.mu.Lock()
	changed := s.needsOnboarding != value
	s.needsOnboarding = value
	s.mu.Unlock()
	if changed {
		s.notifyStateChanged()
	}
}

func (s *DesktopService) onboardingStatus() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsOnboarding
}

// setDogeSyncing 更新账户或公告同步状态，并立即通知前端；状态只存在运行时，不写入配置文件。
func (s *DesktopService) setDogeSyncing(announcement bool, syncing bool) {
	s.mu.Lock()
	if announcement {
		s.announcementSyncing = syncing
	} else {
		s.dogeSyncing = syncing
	}
	s.mu.Unlock()
	s.notifyStateChanged()
}

func (s *DesktopService) setDogeSyncPhase(phase string) {
	s.mu.Lock()
	s.dogeSyncPhase = phase
	s.mu.Unlock()
	s.notifyStateChanged()
}

func (s *DesktopService) dogeSyncStatus() (bool, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dogeSyncing, s.dogeSyncPhase, s.announcementSyncing
}

func (s *DesktopService) updateConfig(mutator func(*config.AppConfig) error) error {
	_, err := s.runtime.UpdateConfig(mutator)
	if err == nil {
		s.notifyStateChanged()
	}
	return err
}
