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
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
	"codexrelay/internal/network"
	"codexrelay/internal/platform"
	"codexrelay/internal/relay"
	"codexrelay/internal/tasknotify"
	"codexrelay/internal/usage"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var applicationVersion = "2.1.1"

const (
	NotificationKindBalance      = "balance"
	NotificationKindSubscription = "subscription"
	NotificationKindAnnouncement = "announcement"
	NotificationKindTokenSwitch  = "token-switch"
	dogeProfileURL               = "https://ergouzi.life/profile"
)

type DesktopState struct {
	Version         string `json:"version"`
	UpdateSupported bool   `json:"updateSupported"`
	NeedsOnboarding bool   `json:"needsOnboarding"`
	DataDirectory   string `json:"dataDirectory"`
	ProxyPort       int    `json:"proxyPort"`
	// ListenOnAllInterfaces 是网络设置页展示的监听范围，不代表当前出站网络出口。
	ListenOnAllInterfaces bool                       `json:"listenOnAllInterfaces"`
	ProxyURL              string                     `json:"proxyUrl"`
	ProxyURLs             map[string]string          `json:"proxyUrls"`
	LocalAccessToken      string                     `json:"localAccessToken"`
	ActiveProfiles        map[string]string          `json:"activeProfiles"`
	Profiles              []PublicProfile            `json:"profiles"`
	FailoverOrder         map[string][]string        `json:"failoverOrder"`
	ClientConfigs         []PublicClientConfig       `json:"clientConfigs"`
	Network               network.Settings           `json:"network"`
	SystemProxy           network.SystemProxyInfo    `json:"systemProxy"`
	Requests              []usage.RequestRecord      `json:"requests"`
	Usage                 usage.Overview             `json:"usage"`
	UptimeSeconds         int64                      `json:"uptimeSeconds"`
	Preferences           config.Preferences         `json:"preferences"`
	TokenSwitch           config.TokenSwitchSettings `json:"tokenSwitch"`
	TaskNotification      TaskNotificationState      `json:"taskNotification"`
	Doge                  DogeState                  `json:"doge"`
}

// TaskNotificationState 是消息通知设置和后台队列的公开快照。通知 URL 仅在投递时
// 替换 {title}、{content}，状态计数和事件选择均不包含 Codex 任务、账户或令牌内容。
type TaskNotificationState struct {
	Enabled               bool                          `json:"enabled"`
	WebhookURL            string                        `json:"webhookUrl"`
	Events                config.TaskNotificationEvents `json:"events"`
	IdleGraceSeconds      int                           `json:"idleGraceSeconds"`
	RequestTimeoutSeconds int                           `json:"requestTimeoutSeconds"`
	MaxAttempts           int                           `json:"maxAttempts"`
	Status                tasknotify.Status             `json:"status"`
}

type DogeState struct {
	BaseURL                       string                                  `json:"baseUrl"`
	Bound                         bool                                    `json:"bound"`
	Account                       PublicDogeAccount                       `json:"account"`
	User                          map[string]any                          `json:"user"`
	WalletUSD                     float64                                 `json:"walletUsd"`
	SubscriptionsUSD              float64                                 `json:"subscriptionsUsd"`
	TotalUSD                      float64                                 `json:"totalUsd"`
	Subscriptions                 []PublicDogeSubscription                `json:"subscriptions"`
	RedemptionEnabled             bool                                    `json:"redemptionEnabled"`
	TopupLink                     string                                  `json:"topupLink"`
	Groups                        []string                                `json:"groups"`
	Tokens                        []PublicDogeToken                       `json:"tokens"`
	Notifications                 PublicDogeNotifications                 `json:"notifications"`
	TokenSwitch                   *PublicDogeTokenSwitchPrompt            `json:"tokenSwitch,omitempty"`
	TokenSwitches                 map[string]*PublicDogeTokenSwitchPrompt `json:"tokenSwitches"`
	BalanceAlertEnabled           bool                                    `json:"balanceAlertEnabled"`
	BalanceAlertThresholdUSD      float64                                 `json:"balanceAlertThresholdUsd"`
	SubscriptionAlertEnabled      bool                                    `json:"subscriptionAlertEnabled"`
	SubscriptionAlertThresholdUSD float64                                 `json:"subscriptionAlertThresholdUsd"`
	Syncing                       bool                                    `json:"syncing"`
	SyncPhase                     string                                  `json:"syncPhase"`
	AnnouncementSyncing           bool                                    `json:"announcementSyncing"`
	SyncIntervalMinutes           int                                     `json:"syncIntervalMinutes"`
	LastSyncAt                    string                                  `json:"lastSyncAt"`
	LastSyncError                 string                                  `json:"lastSyncError"`
}

// PublicDogeAccount 是连接页展示的账户摘要；额度字段统一转换为美元，不包含访问令牌。
type PublicDogeAccount struct {
	UserID       int64   `json:"userId"`
	Nickname     string  `json:"nickname"`
	Email        string  `json:"email"`
	BalanceUSD   float64 `json:"balanceUsd"`
	UsedUSD      float64 `json:"usedUsd"`
	RequestCount int64   `json:"requestCount"`
}

// PublicDogeAnnouncement 是公告面板使用的公开公告内容，不包含访问令牌。
type PublicDogeAnnouncement struct {
	ID          int64  `json:"id"`
	Content     string `json:"content"`
	Extra       string `json:"extra"`
	PublishDate string `json:"publishDate"`
	Type        string `json:"type"`
	Read        bool   `json:"read"`
}

// PublicDogeAlert 是右下角提醒窗使用的单条提醒摘要。
type PublicDogeAlert struct {
	Kind           string  `json:"kind"`
	Key            string  `json:"key"`
	Title          string  `json:"title"`
	Message        string  `json:"message"`
	AnnouncementID int64   `json:"announcementId,omitempty"`
	AmountUSD      float64 `json:"amountUsd,omitempty"`
}

// PublicDogeTokenSwitchCandidate 是令牌切换弹窗展示的候选摘要，不包含完整密钥；Group 始终是 display_name。
type PublicDogeTokenSwitchCandidate struct {
	TokenID        int64   `json:"tokenId"`
	ProfileID      string  `json:"profileId,omitempty"`
	Name           string  `json:"name"`
	Source         string  `json:"source"`
	Group          string  `json:"group"`
	Ratio          float64 `json:"ratio"`
	Selectable     bool    `json:"selectable"`
	DisabledReason string  `json:"disabledReason,omitempty"`
}

// PublicDogeTokenSwitchHistory 是自动轮次中的切换或最终故障记录，不包含 API 密钥或请求正文。
// ToName 为空表示当前令牌失败后没有可切换目标，前端应将 SwitchedAt 显示为故障时间。
type PublicDogeTokenSwitchHistory struct {
	FromName       string `json:"fromName"`
	ToName         string `json:"toName"`
	SwitchedAt     string `json:"switchedAt"`
	FailureMessage string `json:"failureMessage"`
}

// PublicDogeTokenSwitchPrompt 是右下角令牌切换弹窗的只读状态。
type PublicDogeTokenSwitchPrompt struct {
	Key                  string                           `json:"key"`
	Category             string                           `json:"category"`
	Mode                 string                           `json:"mode"`
	FailureKind          string                           `json:"failureKind"`
	FailureCount         int                              `json:"failureCount"`
	FailureStatus        int                              `json:"failureStatus,omitempty"`
	FailureWindowMinutes int                              `json:"failureWindowMinutes,omitempty"`
	CurrentTokenID       int64                            `json:"currentTokenId"`
	CurrentProfileID     string                           `json:"currentProfileId"`
	CurrentName          string                           `json:"currentName"`
	CurrentGroup         string                           `json:"currentGroup"`
	CurrentRatio         float64                          `json:"currentRatio"`
	Message              string                           `json:"message"`
	SwitchedToName       string                           `json:"switchedToName,omitempty"`
	Candidates           []PublicDogeTokenSwitchCandidate `json:"candidates"`
	SwitchHistory        []PublicDogeTokenSwitchHistory   `json:"switchHistory,omitempty"`
	Stopped              bool                             `json:"stopped,omitempty"`
	StoppedAt            string                           `json:"stoppedAt,omitempty"`
	StopMessage          string                           `json:"stopMessage,omitempty"`
}

// PublicDogeNotifications 是公告中心和提醒窗口共用的只读快照。
type PublicDogeNotifications struct {
	Initialized   bool                     `json:"initialized"`
	Enabled       bool                     `json:"enabled"`
	CurrentNotice string                   `json:"currentNotice"`
	Announcements []PublicDogeAnnouncement `json:"announcements"`
	UnreadCount   int                      `json:"unreadCount"`
	Alerts        []PublicDogeAlert        `json:"alerts"`
	LastSyncAt    string                   `json:"lastSyncAt"`
	LastSyncError string                   `json:"lastSyncError"`
	Syncing       bool                     `json:"syncing"`
}

// PublicDogeSubscription 是提供给界面的套餐剩余摘要，金额统一转换为美元。
type PublicDogeSubscription struct {
	ID           int64   `json:"id"`
	PlanID       int64   `json:"planId"`
	PlanTitle    string  `json:"planTitle"`
	Status       string  `json:"status"`
	RemainingUSD float64 `json:"remainingUsd"`
	EndTime      int64   `json:"endTime"`
}

// PublicDogeToken 是主页令牌目录的只读摘要；Group 保留原始分组键供内部状态使用，界面只读取 GroupDisplayName。
// Permitted 表示令牌状态、分组权限和本地完整密钥均满足切换约束，不代表已经导入 Profile。
type PublicDogeToken struct {
	ID               int64   `json:"id"`
	MaskedKey        string  `json:"maskedKey"`
	OrderKey         string  `json:"orderKey"`
	Status           int     `json:"status"`
	Name             string  `json:"name"`
	CreatedTime      int64   `json:"createdTime"`
	AccessedTime     int64   `json:"accessedTime"`
	ExpiredTime      int64   `json:"expiredTime"`
	RemainQuota      int64   `json:"remainQuota"`
	UnlimitedQuota   bool    `json:"unlimitedQuota"`
	UsedQuota        int64   `json:"usedQuota"`
	Group            string  `json:"group"`
	GroupDisplayName string  `json:"groupDisplayName"`
	GroupRatio       float64 `json:"groupRatio"`
	Category         string  `json:"category"`
	Note             string  `json:"note"`
	NeedsCategory    bool    `json:"needsCategory"`
	Permitted        bool    `json:"permitted"`
	Imported         bool    `json:"imported"`
	ProfileID        string  `json:"profileId"`
	Active           bool    `json:"active"`
}

// DogeTokenCategoryInput 描述一个二狗子令牌的本地存放类别选择；ID 必须来自当前同步目录。
type DogeTokenCategoryInput struct {
	ID       int64  `json:"id"`
	Category string `json:"category"`
}

type PublicProfile struct {
	ID             string            `json:"id"`
	Source         string            `json:"source"`
	Category       string            `json:"category"`
	Name           string            `json:"name"`
	BaseURL        string            `json:"baseUrl"`
	APIKey         string            `json:"apiKey"`
	Note           string            `json:"note,omitempty"`
	Headers        map[string]string `json:"headers"`
	Models         []PublicModel     `json:"models"`
	DefaultModel   string            `json:"defaultModel"`
	Active         bool              `json:"active"`
	PreviewURL     string            `json:"previewUrl"`
	RemoteTokenID  int64             `json:"remoteTokenId,omitempty"`
	SkipAutoSwitch bool              `json:"skipAutoSwitch"`
}

// PublicModel 是编辑页模型管理器展示的模型摘要，不包含密钥或请求正文。
type PublicModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OwnedBy       string `json:"ownedBy"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
}

// ModelInput 是前端保存模型目录时提交的单条模型；ID 必须是用户确认的真实模型 ID。
type ModelInput struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OwnedBy       string `json:"ownedBy"`
	ContextWindow int64  `json:"contextWindow"`
}

type ProfileInput struct {
	ID           string            `json:"id"`
	Source       string            `json:"source"`
	Category     string            `json:"category"`
	Name         string            `json:"name"`
	BaseURL      string            `json:"baseUrl"`
	APIKey       string            `json:"apiKey"`
	Note         string            `json:"note"`
	Headers      map[string]string `json:"headers"`
	Models       []ModelInput      `json:"models"`
	DefaultModel string            `json:"defaultModel"`
}

type TestResult struct {
	OK         bool   `json:"ok"`
	Reachable  bool   `json:"reachable"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"durationMs"`
	URL        string `json:"url"`
}

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

// proxyListenAddress 返回透明代理的本地监听地址；默认只接受 Windows 本机回环请求，
// 开启 WSL2 访问后改为监听所有 IPv4 网卡。监听范围不改变本地访问令牌校验。
func proxyListenAddress(port int, listenOnAllInterfaces bool) string {
	host := "127.0.0.1"
	if listenOnAllInterfaces {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func (s *DesktopService) prepareProxyListener(port int, listenOnAllInterfaces bool) (net.Listener, *http.Server, error) {
	address := proxyListenAddress(port, listenOnAllInterfaces)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("代理端口 %s 无法监听，可能已有实例正在运行: %w", address, err)
	}
	server := &http.Server{
		Handler: s.runtime.ProxyHandler(), ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
	return listener, server, nil
}

func (s *DesktopService) serveProxy(server *http.Server, listener net.Listener) {
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			application.Get().Logger.Error("透明代理异常退出", "error", err)
		}
	}()
}

// installProxyListener 替换当前监听；调用方须先完成新监听绑定和配置持久化。
// 新监听启动后旧服务再优雅退出，避免改端口时出现不必要的代理空窗。
func (s *DesktopService) installProxyListener(server *http.Server, listener net.Listener) bool {
	s.mu.Lock()
	oldServer := s.server
	oldListener := s.listener
	if oldServer == nil && oldListener == nil {
		s.mu.Unlock()
		return false
	}
	s.server = server
	s.listener = listener
	s.mu.Unlock()
	s.serveProxy(server, listener)
	if oldServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := oldServer.Shutdown(ctx); err != nil {
			application.Get().Logger.Error("旧监听端口关闭失败", "error", err)
		}
		cancel()
	} else if oldListener != nil {
		_ = oldListener.Close()
	}
	return true
}

func (s *DesktopService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("代理尚未初始化")
	}
	// 启动时只探测各客户端的已知配置目录；延迟存储会在首次初始化完成后再落盘。
	if err := s.scanClientConfigs(); err != nil {
		application.Get().Logger.Warn("扫描外部客户端配置目录失败", "error", err)
	}
	listener, server, err := s.prepareProxyListener(state.Config.ProxyPort, state.Config.ListenOnAllInterfaces)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = listener
	s.server = server
	s.mu.Unlock()
	s.serveProxy(server, listener)
	s.mu.Lock()
	if !s.taskNotifyStarted {
		s.taskNotifyStarted = true
		ctx, cancel := context.WithCancel(context.Background())
		s.taskNotifyCancel = cancel
		s.mu.Unlock()
		s.taskNotifier.Start(ctx)
	} else {
		s.mu.Unlock()
	}
	s.mu.Lock()
	if !s.syncStarted {
		s.syncStarted = true
		ctx, cancel := context.WithCancel(context.Background())
		s.syncCancel = cancel
		s.mu.Unlock()
		go s.dogeSyncLoop(ctx)
		if strings.TrimSpace(state.Config.Doge.AccessToken) != "" {
			// 启动恢复只刷新目录元数据；已有密钥来自本地缓存，新令牌才按需补全。
			go func() { _ = s.syncDoge(context.Background(), "", false, dogeSyncMetadata) }()
		} else {
			go func() { _ = s.SyncDogeAnnouncements() }()
		}
	} else {
		s.mu.Unlock()
	}
	return nil
}

func (s *DesktopService) ServiceShutdown() error {
	s.mu.Lock()
	server := s.server
	cancel := s.syncCancel
	taskNotifyCancel := s.taskNotifyCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if taskNotifyCancel != nil {
		taskNotifyCancel()
	}
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

func (s *DesktopService) GetState() DesktopState {
	state := s.runtime.State()
	if state == nil {
		return DesktopState{}
	}
	dogeSyncing, dogeSyncPhase, announcementSyncing := s.dogeSyncStatus()
	profiles := make([]PublicProfile, 0, len(state.Config.Profiles))
	for _, profile := range state.Config.Profiles {
		profiles = append(profiles, publicProfile(profile, state.Config.ActiveProfiles))
	}
	profilesByRemoteID := make(map[int64]PublicProfile)
	for _, profile := range profiles {
		if profile.RemoteTokenID > 0 {
			profilesByRemoteID[profile.RemoteTokenID] = profile
		}
	}
	dogeTokens := make([]PublicDogeToken, 0, len(state.Config.Doge.Tokens))
	for _, token := range state.Config.Doge.Tokens {
		imported := profilesByRemoteID[token.ID]
		category := token.Category
		if category == "" && imported.ID != "" {
			category = imported.Category
		}
		masked := token.MaskedKey
		if masked == "" {
			masked = maskDogeKey(token.Key)
		}
		masked = normalizeDogeAPIKey(masked)
		note := token.Note
		if imported.Note != "" && !strings.HasPrefix(imported.Note, "二狗子 · 分组：") {
			note = imported.Note
		}
		if note == "" {
			note = dogeTokenNote(token)
		}
		dogeTokens = append(dogeTokens, PublicDogeToken{
			ID: token.ID, MaskedKey: masked, OrderKey: dogeTokenOrderKey(token), Status: token.Status, Name: token.Name,
			CreatedTime: token.CreatedTime, AccessedTime: token.AccessedTime, ExpiredTime: token.ExpiredTime,
			RemainQuota: token.RemainQuota, UnlimitedQuota: token.UnlimitedQuota, UsedQuota: token.UsedQuota,
			Group: token.Group, GroupDisplayName: token.GroupDisplayName, GroupRatio: token.GroupRatio,
			Category: category, Note: note, NeedsCategory: category == "", Permitted: dogeTokenSwitchable(token, state.Config.Doge.Groups),
			Imported: imported.ID != "", ProfileID: imported.ID, Active: imported.Active,
		})
	}
	proxyURLs := make(map[string]string, len(config.Categories))
	for _, category := range config.Categories {
		proxyURLs[category] = fmt.Sprintf("http://127.0.0.1:%d/%s", state.Config.ProxyPort, category)
	}
	publicSubscriptions := make([]PublicDogeSubscription, 0, len(state.Config.Doge.Subscriptions))
	notificationSubscriptions := make([]PublicDogeSubscription, 0, len(state.Config.Doge.Subscriptions))
	subscriptionsUSD := 0.0
	for _, subscription := range state.Config.Doge.Subscriptions {
		remaining := subscription.AmountTotal - subscription.AmountUsed
		remainingUSD := dogeQuotaToUSD(remaining)
		publicSubscription := PublicDogeSubscription{ID: subscription.ID, PlanID: subscription.PlanID, PlanTitle: subscription.PlanTitle, Status: subscription.Status, RemainingUSD: remainingUSD, EndTime: subscription.EndTime}
		notificationSubscriptions = append(notificationSubscriptions, publicSubscription)
		if !isDogeSubscriptionActive(subscription, time.Now()) {
			continue
		}
		subscriptionsUSD += remainingUSD
		publicSubscriptions = append(publicSubscriptions, publicSubscription)
	}
	walletUSD := dogeQuotaToUSD(state.Config.Doge.Account.Quota)
	account := PublicDogeAccount{
		UserID: state.Config.Doge.Account.ID, Nickname: state.Config.Doge.Account.DisplayName,
		Email: state.Config.Doge.Account.Email, BalanceUSD: walletUSD,
		UsedUSD: dogeQuotaToUSD(state.Config.Doge.Account.UsedQuota), RequestCount: state.Config.Doge.Account.RequestCount,
	}
	dogeState := DogeState{
		BaseURL: state.Config.Doge.BaseURL, Bound: strings.TrimSpace(state.Config.Doge.AccessToken) != "", Account: account,
		WalletUSD: walletUSD, SubscriptionsUSD: subscriptionsUSD, TotalUSD: walletUSD + subscriptionsUSD,
		Subscriptions: publicSubscriptions, RedemptionEnabled: state.Config.Doge.Topup.EnableRedemption, TopupLink: state.Config.Doge.Topup.TopupLink,
		User: state.Config.Doge.User, Groups: append([]string(nil), state.Config.Doge.Groups...), Tokens: dogeTokens,
		Notifications:       publicDogeNotifications(state.Config.Doge, walletUSD, notificationSubscriptions, announcementSyncing),
		BalanceAlertEnabled: state.Config.Doge.Notifications.BalanceAlertEnabled, BalanceAlertThresholdUSD: state.Config.Doge.Notifications.BalanceAlertThresholdUSD,
		SubscriptionAlertEnabled: state.Config.Doge.Notifications.SubscriptionAlertEnabled, SubscriptionAlertThresholdUSD: state.Config.Doge.Notifications.SubscriptionAlertThresholdUSD,
		Syncing:             dogeSyncing,
		SyncPhase:           dogeSyncPhase,
		AnnouncementSyncing: announcementSyncing,
		SyncIntervalMinutes: state.Config.Doge.SyncIntervalMinutes, LastSyncError: state.Config.Doge.LastSyncError,
	}
	dogeState.TokenSwitches = s.currentTokenSwitchPrompts()
	dogeState.TokenSwitch = firstTokenSwitchPrompt(dogeState.TokenSwitches)
	if !state.Config.Doge.LastSyncAt.IsZero() {
		dogeState.LastSyncAt = state.Config.Doge.LastSyncAt.Format(time.RFC3339)
	}
	return DesktopState{
		Version: applicationVersion, UpdateSupported: updatesSupported(), NeedsOnboarding: s.onboardingStatus(), DataDirectory: s.runtime.DataDirectory(), ProxyPort: state.Config.ProxyPort, ListenOnAllInterfaces: state.Config.ListenOnAllInterfaces,
		ProxyURL: proxyURLs[config.CategoryCodex], ProxyURLs: proxyURLs,
		LocalAccessToken: state.Config.LocalAccessToken, ActiveProfiles: state.Config.ActiveProfiles,
		Profiles: profiles, FailoverOrder: config.NormalizeFailoverOrder(state.Config.FailoverOrder, state.Config.Profiles), ClientConfigs: publicClientConfigs(state.Config), Network: state.Config.Network, SystemProxy: state.SystemProxy,
		Requests: s.runtime.RecentRecords(), Usage: s.runtime.UsageOverview(), UptimeSeconds: int64(s.runtime.Uptime().Seconds()),
		Preferences: state.Config.Preferences, TokenSwitch: state.Config.TokenSwitch,
		TaskNotification: publicTaskNotification(state.Config.TaskNotification, s.taskNotifier.Status()),
		Doge:             dogeState,
	}
}

func publicTaskNotification(setting config.TaskNotification, status tasknotify.Status) TaskNotificationState {
	setting = config.NormalizeTaskNotification(setting)
	return TaskNotificationState{Enabled: setting.Enabled, WebhookURL: setting.WebhookURL, Events: setting.Events, IdleGraceSeconds: setting.IdleGraceSeconds, RequestTimeoutSeconds: setting.RequestTimeoutSeconds, MaxAttempts: setting.MaxAttempts, Status: status}
}

// enqueueTaskNotificationEvent 只把已确认的本地事件交给后台耐久队列。投递和重试
// 由 watcher 的周期 worker 执行，不能在代理请求或二狗子同步调用栈中等待网络结果。
func (s *DesktopService) enqueueTaskNotificationEvent(eventType, identity string, details ...tasknotify.EventDetails) {
	if s.taskNotifier == nil {
		return
	}
	if err := s.taskNotifier.Enqueue(eventType, identity, details...); err != nil {
		application.Get().Logger.Warn("创建消息通知待投递记录失败", "error", err)
	}
}

// handleHealthChanged 在健康阈值首次触发时记录独立故障通知，再按设置执行自动切换；失败轮次耗尽后保留停止状态提醒。
// 回调由 relay 在请求完成后调用，因此只改变后续请求使用的活动 Profile，不重放已经失败的请求。
func (s *DesktopService) handleHealthChanged() {
	s.enqueueTokenRequestFailureNotifications()
	s.failoverMu.Lock()
	prompts := s.buildTokenSwitchPrompts()
	previousIDs := make([]string, 0, len(prompts))
	state := s.runtime.State()
	if state != nil && state.Config.TokenSwitch.Mode == config.TokenSwitchModeAuto {
		for _, category := range config.Categories {
			prompt := prompts[category]
			if switched, previousID := s.tryAutomaticTokenSwitch(prompt); switched {
				previousIDs = append(previousIDs, previousID)
			}
		}
	}
	s.failoverMu.Unlock()
	for _, previousID := range previousIDs {
		s.runtime.ResetProfileHealth(previousID)
	}
	s.notifyStateChanged()
}

// enqueueTokenRequestFailureNotifications 为每个达到健康阈值的活动 Profile 创建一条独立通知。
// 该事件不依赖自动切换模式；身份包含触发代数，保证同一故障轮次不会因状态刷新重复入队。
// 只保存类别、故障类别、次数、状态码和时间，不保存令牌名称、密钥或错误正文。
func (s *DesktopService) enqueueTokenRequestFailureNotifications() {
	state := s.runtime.State()
	if state == nil {
		return
	}
	for _, health := range s.runtime.HealthSnapshots() {
		failureKind := ""
		switch {
		case health.AuthTriggered:
			failureKind = "auth"
		case health.UpstreamTriggered:
			failureKind = "upstream"
		default:
			continue
		}
		profileIndex := config.FindProfileIndex(state.Config.Profiles, health.ProfileID)
		if profileIndex < 0 || state.Config.ActiveProfiles[health.Category] != health.ProfileID {
			continue
		}
		identity := strings.Join([]string{health.ProfileID, failureKind, strconv.Itoa(health.LastStatus), strconv.FormatUint(health.TriggerGeneration, 10)}, "\x00")
		s.enqueueTaskNotificationEvent(tasknotify.EventTokenRequestFailed, identity, tasknotify.EventDetails{
			OccurredAt: health.LastFailureAt, Category: health.Category, FailureKind: failureKind,
			FailureCount: func() int {
				if failureKind == "auth" {
					return health.AuthFailures
				}
				return health.UpstreamFailures
			}(), FailureStatus: health.LastStatus, FailureWindowMinutes: state.Config.TokenSwitch.UpstreamFailureWindowMinutes,
		})
	}
}

// handleUpstreamResult 在当前活动令牌真正成功后结束该类别的故障轮次，并刷新已显示的手动故障提示。
// 自动切换成功后的通知继续保留到用户确认；这里只清理尝试集合，使后续新故障可以开始新一轮。
func (s *DesktopService) handleUpstreamResult(profileID, category string, status int, transportError bool) {
	if transportError || status >= 400 || profileID == "" || category == "" {
		return
	}
	state := s.runtime.State()
	if state == nil || state.Config.ActiveProfiles[category] != profileID {
		return
	}
	s.switchMu.Lock()
	_, clearedRound := s.switchRounds[category]
	clearedPrompt := false
	for key := range s.switchPrompts {
		if strings.HasPrefix(key, profileID+"|") {
			delete(s.switchPrompts, key)
			clearedPrompt = true
		}
	}
	if clearedRound {
		delete(s.switchRounds, category)
	}
	s.switchMu.Unlock()
	// 手动模式没有 switchRounds；仍须在成功响应清掉已显示的故障提示并刷新独立窗口。
	if clearedRound || clearedPrompt {
		s.notifyStateChanged()
	}
}

// tryAutomaticTokenSwitch 在同一故障轮次中依次尝试未尝试的候选。
// 候选切换失败也会计入尝试集合；全部候选耗尽后只生成一次停止提示，不再回到列表开头。
func (s *DesktopService) tryAutomaticTokenSwitch(prompt *PublicDogeTokenSwitchPrompt) (bool, string) {
	if prompt == nil {
		return false, ""
	}
	s.ensureTokenSwitchRound(prompt.Category)
	s.resumeTokenSwitchRound(prompt)
	s.markTokenSwitchAttempt(prompt.Category, prompt.CurrentProfileID)
	for _, candidate := range prompt.Candidates {
		if !candidate.Selectable || candidate.ProfileID == "" {
			continue
		}
		if s.tokenSwitchAttempted(prompt.Category, candidate.ProfileID) {
			continue
		}
		s.markTokenSwitchAttempt(prompt.Category, candidate.ProfileID)
		if err := s.switchProfile(prompt.Category, prompt.CurrentProfileID, candidate.ProfileID, tokenSwitchCurrentWasRemoved(s.runtime.State(), prompt)); err != nil {
			continue
		}
		s.recordTokenSwitch(prompt.Category, prompt.CurrentName, candidate.Name, historyFailureMessage(prompt))
		s.clearSwitchPrompt(prompt.Key)
		s.setAutoSwitchNotice(prompt, candidate.Name)
		s.enqueueTaskNotificationEvent(tasknotify.EventTokenAutoSwitched, fmt.Sprintf("%s\x00%s\x00%d", prompt.Key, candidate.ProfileID, time.Now().UnixNano()), tasknotify.EventDetails{
			OccurredAt: time.Now(), Category: prompt.Category, FromGroup: prompt.CurrentGroup, ToGroup: candidate.Group,
		})
		return true, prompt.CurrentProfileID
	}

	s.stopTokenSwitchRound(prompt)
	return false, ""
}

// resumeTokenSwitchRound 只在真正准备自动尝试且出现未尝试候选时恢复已停止轮次。
// 状态读取和窗口渲染不得修改轮次，避免配置保存通知与自动执行之间短暂显示错误模式。
func (s *DesktopService) resumeTokenSwitchRound(prompt *PublicDogeTokenSwitchPrompt) {
	if prompt == nil {
		return
	}
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	round := s.switchRounds[prompt.Category]
	if round == nil || !round.Stopped {
		return
	}
	hasCandidate := false
	for _, candidate := range prompt.Candidates {
		if candidate.Selectable && candidate.ProfileID != "" {
			hasCandidate = true
			break
		}
	}
	if !hasCandidate {
		return
	}
	round.Stopped = false
	round.StoppedAt = time.Time{}
	round.StopMessage = ""
	delete(s.autoSwitchNotices, prompt.Category)
}

func (s *DesktopService) ensureTokenSwitchRound(category string) *tokenSwitchRound {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	if s.switchRounds == nil {
		s.switchRounds = make(map[string]*tokenSwitchRound)
	}
	round := s.switchRounds[category]
	if round == nil {
		round = &tokenSwitchRound{AttemptedIDs: make(map[string]struct{})}
		s.switchRounds[category] = round
	}
	return round
}

func (s *DesktopService) markTokenSwitchAttempt(category, profileID string) {
	if strings.TrimSpace(profileID) == "" {
		return
	}
	round := s.ensureTokenSwitchRound(category)
	s.switchMu.Lock()
	if round.AttemptedIDs == nil {
		round.AttemptedIDs = make(map[string]struct{})
	}
	round.AttemptedIDs[profileID] = struct{}{}
	s.switchMu.Unlock()
}

func (s *DesktopService) tokenSwitchAttempted(category, profileID string) bool {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	round := s.switchRounds[category]
	if round == nil {
		return false
	}
	_, ok := round.AttemptedIDs[profileID]
	return ok
}

func (s *DesktopService) recordTokenSwitch(category, fromName, toName, failureMessage string) {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	round := s.switchRounds[category]
	if round == nil {
		return
	}
	round.History = append(round.History, PublicDogeTokenSwitchHistory{
		FromName: fromName, ToName: toName, SwitchedAt: time.Now().Format("2006-01-02 15:04:05"), FailureMessage: failureMessage,
	})
}

func (s *DesktopService) stopTokenSwitchRound(prompt *PublicDogeTokenSwitchPrompt) {
	if prompt == nil {
		return
	}
	switchMu := &s.switchMu
	switchMu.Lock()
	if s.switchRounds == nil {
		s.switchRounds = make(map[string]*tokenSwitchRound)
	}
	round := s.switchRounds[prompt.Category]
	if round == nil {
		round = &tokenSwitchRound{AttemptedIDs: make(map[string]struct{})}
		s.switchRounds[prompt.Category] = round
	}
	if !round.Stopped {
		round.History = append(round.History, PublicDogeTokenSwitchHistory{
			FromName:       prompt.CurrentName,
			SwitchedAt:     time.Now().Format("2006-01-02 15:04:05"),
			FailureMessage: historyFailureMessage(prompt),
		})
	}
	round.Stopped = true
	round.StoppedAt = time.Now()
	round.StopMessage = fmt.Sprintf("当前类别暂无可用令牌，已停止自动切换，避免重复使用故障令牌。")
	notice := *prompt
	notice.Mode = "auto"
	notice.Stopped = true
	notice.StoppedAt = round.StoppedAt.Format("2006-01-02 15:04:05")
	notice.StopMessage = round.StopMessage
	notice.Message = "本轮令牌均已尝试，自动切换已停止。"
	notice.Candidates = nil
	notice.SwitchHistory = append([]PublicDogeTokenSwitchHistory(nil), round.History...)
	if s.autoSwitchNotices == nil {
		s.autoSwitchNotices = make(map[string]*PublicDogeTokenSwitchPrompt)
	}
	s.autoSwitchNotices[prompt.Category] = &notice
	switchMu.Unlock()
	s.enqueueTaskNotificationEvent(tasknotify.EventTokenAutoSwitchFailed, fmt.Sprintf("%s\x00%d", prompt.Key, time.Now().UnixNano()), tasknotify.EventDetails{
		OccurredAt: time.Now(), Category: prompt.Category, FromGroup: prompt.CurrentGroup, ToGroup: "无可用分组",
	})
}

func historyFailureMessage(prompt *PublicDogeTokenSwitchPrompt) string {
	if prompt == nil {
		return "上游请求异常"
	}
	switch prompt.FailureKind {
	case "auth":
		return fmt.Sprintf("连续 %d 次返回 HTTP %d", prompt.FailureCount, prompt.FailureStatus)
	case "directory":
		if prompt.Message != "" {
			return prompt.Message
		}
		return "令牌目录状态异常"
	default:
		return fmt.Sprintf("%d 分钟内累计 %d 次上游异常", promptFailureWindow(prompt), prompt.FailureCount)
	}
}

func promptFailureWindow(prompt *PublicDogeTokenSwitchPrompt) int {
	if prompt == nil || prompt.FailureWindowMinutes <= 0 {
		return config.DefaultUpstreamFailureWindowMinutes
	}
	return prompt.FailureWindowMinutes
}

func (s *DesktopService) clearSwitchPrompt(key string) {
	s.switchMu.Lock()
	delete(s.switchPrompts, key)
	// 目录失效快照不能在切换成功后立即删除：下一次同步需要用它对比旧状态，
	// 才能在令牌状态或分组恢复时生成独立提醒。setDogeDirectorySwitchContexts
	// 会在下一次同步中按最新目录替换或清理该快照。
	for category, notice := range s.directoryRecoveryNotices {
		if notice != nil && notice.Key == key {
			delete(s.directoryRecoveryNotices, category)
		}
	}
	s.switchMu.Unlock()
}

// currentTokenSwitchPrompts 返回每个类别当前应显示的独立令牌状态。
// 同类别自动切换结果覆盖该类别的手动提示，其他类别互不覆盖；所有状态只在本次运行内保留。
func (s *DesktopService) currentTokenSwitchPrompts() map[string]*PublicDogeTokenSwitchPrompt {
	prompts := s.buildTokenSwitchPrompts()
	state := s.runtime.State()
	if state == nil || state.Config.TokenSwitch.Mode != config.TokenSwitchModeAuto {
		return prompts
	}
	s.switchMu.Lock()
	for category, notice := range s.autoSwitchNotices {
		if notice == nil {
			continue
		}
		if prompt := prompts[category]; prompt != nil && prompt.FailureKind == "directory_recovered" {
			continue
		}
		clone := *notice
		clone.SwitchHistory = append([]PublicDogeTokenSwitchHistory(nil), notice.SwitchHistory...)
		prompts[category] = &clone
	}
	s.switchMu.Unlock()
	return prompts
}

func firstTokenSwitchPrompt(prompts map[string]*PublicDogeTokenSwitchPrompt) *PublicDogeTokenSwitchPrompt {
	for _, category := range config.Categories {
		if prompt := prompts[category]; prompt != nil {
			return prompt
		}
	}
	return nil
}

// setAutoSwitchNotice 保存自动切换成功结果，供独立令牌提醒窗口渲染。
// prompt 来自切换前的候选快照，targetName 必须是经过统一格式化的目标名称。
func (s *DesktopService) setAutoSwitchNotice(prompt *PublicDogeTokenSwitchPrompt, targetName string) {
	if prompt == nil {
		return
	}
	notice := *prompt
	notice.Mode = "auto"
	notice.SwitchedToName = strings.TrimSpace(targetName)
	notice.Message = autoSwitchMessage(prompt, notice.SwitchedToName)
	notice.Candidates = nil
	s.switchMu.Lock()
	if round := s.switchRounds[prompt.Category]; round != nil {
		notice.SwitchHistory = append([]PublicDogeTokenSwitchHistory(nil), round.History...)
		notice.Stopped = round.Stopped
		notice.StoppedAt = ""
		notice.StopMessage = round.StopMessage
	}
	if s.autoSwitchNotices == nil {
		s.autoSwitchNotices = make(map[string]*PublicDogeTokenSwitchPrompt)
	}
	s.autoSwitchNotices[prompt.Category] = &notice
	s.switchMu.Unlock()
}

func autoSwitchMessage(prompt *PublicDogeTokenSwitchPrompt, targetName string) string {
	currentName := prompt.CurrentName
	if currentName == "" {
		currentName = "当前代理 API"
	}
	if targetName == "" {
		targetName = "下一个可用令牌"
	}
	var failure string
	switch prompt.FailureKind {
	case "auth":
		failure = fmt.Sprintf("连续 %d 次返回 HTTP %d，已达到故障阈值。", prompt.FailureCount, prompt.FailureStatus)
	case "directory":
		failure = "已从最新令牌目录中消失，已达到故障阈值。"
	default:
		failure = fmt.Sprintf("在设定的异常统计窗口内出现 %d 次上游异常，已达到故障阈值。", prompt.FailureCount)
	}
	return fmt.Sprintf("当前 %s %s\n已自动切换至 %s。", currentName, failure, targetName)
}

// CompleteOnboarding 结束首次启动引导并启用便携数据持久化；跳过和绑定成功都调用此方法。
func (s *DesktopService) CompleteOnboarding() error {
	if err := s.runtime.ActivatePortablePersistence(); err != nil {
		return fmt.Errorf("保存首次初始化数据: %w", err)
	}
	s.setNeedsOnboarding(false)
	return nil
}

// buildDogeTokenSwitchPrompt 保留原绑定名称，实际构建所有来源共用的令牌切换提示。
func (s *DesktopService) buildDogeTokenSwitchPrompt() *PublicDogeTokenSwitchPrompt {
	return s.buildTokenSwitchPrompt()
}

// buildTokenSwitchPrompt 保留单条提示入口，供现有调用和测试读取类别顺序中的第一条提示。
func (s *DesktopService) buildTokenSwitchPrompt() *PublicDogeTokenSwitchPrompt {
	return firstTokenSwitchPrompt(s.buildTokenSwitchPrompts())
}

// buildTokenSwitchPrompts 根据运行时健康快照和二狗子目录状态为每个类别构建一条提示。
// 候选只限制当前 API 类别，来源不参与筛选；顺序来自该类别的 FailoverOrder。
// 用户取消后的抑制状态只保存在内存中，失败状态恢复后会被清理。
func (s *DesktopService) buildTokenSwitchPrompts() map[string]*PublicDogeTokenSwitchPrompt {
	result := make(map[string]*PublicDogeTokenSwitchPrompt)
	state := s.runtime.State()
	if state == nil {
		return result
	}

	snapshots := s.runtime.HealthSnapshots()
	type triggeredContext struct {
		context  tokenSwitchContext
		priority int
	}
	contexts := make([]triggeredContext, 0, len(snapshots))
	activeKeys := make(map[string]struct{}, len(snapshots))
	for _, directoryContext := range s.dogeDirectorySwitchContexts() {
		if directoryContext != nil && directoryTriggerEnabled(state.Config.TokenSwitch, directoryContext.directoryReason) &&
			directorySwitchContextApplies(state.Config, directoryContext) {
			directoryContext.tokens = append([]config.DogeToken(nil), state.Config.Doge.Tokens...)
			directoryContext.groups = append([]string(nil), state.Config.Doge.Groups...)
			directoryContext.candidateProfiles = directoryFailoverCandidates(state.Config, directoryContext)
			activeKeys[directoryContext.key] = struct{}{}
			contexts = append(contexts, triggeredContext{context: *directoryContext, priority: -1})
		}
	}
	for _, health := range snapshots {
		failureKind, failureCount, priority := "", 0, 0
		switch {
		case health.AuthTriggered:
			failureKind, failureCount, priority = "auth", health.AuthFailures, 0
		case health.UpstreamTriggered:
			failureKind, failureCount, priority = "upstream", health.UpstreamFailures, 1
		default:
			continue
		}
		profileIndex := config.FindProfileIndex(state.Config.Profiles, health.ProfileID)
		if profileIndex < 0 {
			continue
		}
		profile := state.Config.Profiles[profileIndex]
		if state.Config.ActiveProfiles[profile.Category] != profile.ID {
			continue
		}
		key := profile.ID + "|" + failureKind + "|" + strconv.Itoa(health.LastStatus) + "|" + strconv.FormatUint(health.TriggerGeneration, 10)
		activeKeys[key] = struct{}{}
		ctx := tokenSwitchContext{
			key: key, failureKind: failureKind, failureCount: failureCount, failureStatus: health.LastStatus,
			failureWindowMinutes: state.Config.TokenSwitch.UpstreamFailureWindowMinutes,
			health:               health, profile: profile, token: dogeTokenForProfile(state.Config, profile),
			tokens: append([]config.DogeToken(nil), state.Config.Doge.Tokens...), groups: append([]string(nil), state.Config.Doge.Groups...),
			candidateProfiles: s.failoverCandidates(profile.Category, profile.ID, state.Config.TokenSwitch.Loop),
		}
		contexts = append(contexts, triggeredContext{context: ctx, priority: priority})
	}

	switchPromptStates := s.switchPromptStates(activeKeys)
	sort.Slice(contexts, func(i, j int) bool {
		if contexts[i].priority != contexts[j].priority {
			return contexts[i].priority < contexts[j].priority
		}
		return contexts[i].context.key < contexts[j].context.key
	})
	for _, item := range contexts {
		if dismissed, ok := switchPromptStates[item.context.key]; ok && dismissed {
			continue
		}
		category := item.context.profile.Category
		if result[category] == nil {
			result[category] = s.applyTokenSwitchRound(publicDogeTokenSwitchPrompt(item.context), state.Config.TokenSwitch.Mode == config.TokenSwitchModeAuto)
		}
	}
	s.switchMu.Lock()
	for category, notice := range s.directoryRecoveryNotices {
		if notice == nil {
			continue
		}
		if state.Config.ActiveProfiles[category] != notice.CurrentProfileID || result[category] != nil {
			continue
		}
		clone := *notice
		clone.Candidates = append([]PublicDogeTokenSwitchCandidate(nil), notice.Candidates...)
		result[category] = &clone
	}
	s.switchMu.Unlock()
	return result
}

// applyTokenSwitchRound 只在自动模式下合并当前类别的历史并过滤本轮已尝试候选。
// 手动提示不属于自动故障轮次，用户每次收到提示时都能按当前列表选择任意可用候选。
func (s *DesktopService) applyTokenSwitchRound(prompt *PublicDogeTokenSwitchPrompt, automatic bool) *PublicDogeTokenSwitchPrompt {
	if prompt == nil {
		return nil
	}
	if !automatic {
		return prompt
	}
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	round := s.switchRounds[prompt.Category]
	if round == nil {
		return prompt
	}
	filtered := make([]PublicDogeTokenSwitchCandidate, 0, len(prompt.Candidates))
	for _, candidate := range prompt.Candidates {
		if _, attempted := round.AttemptedIDs[candidate.ProfileID]; attempted {
			continue
		}
		filtered = append(filtered, candidate)
	}
	prompt.Candidates = filtered
	prompt.SwitchHistory = append([]PublicDogeTokenSwitchHistory(nil), round.History...)
	prompt.Stopped = round.Stopped
	if !round.Stopped && len(filtered) == 0 {
		prompt.Stopped = true
	}
	if round.Stopped || prompt.Stopped {
		prompt.StopMessage = round.StopMessage
		if prompt.StopMessage == "" {
			prompt.StopMessage = "当前类别暂无可用令牌，已停止自动切换，避免重复使用故障令牌。"
		}
	}
	return prompt
}

// dogeDirectorySwitchContexts 返回按类别保存的目录失效快照。
// 快照只保留同步前当前令牌和顺序锚点；候选始终按调用时配置重算，且这些状态都不写入配置文件。
func (s *DesktopService) dogeDirectorySwitchContexts() map[string]*tokenSwitchContext {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	result := make(map[string]*tokenSwitchContext, len(s.directorySwitches))
	for category, source := range s.directorySwitches {
		if source == nil {
			continue
		}
		context := *source
		context.tokens = append([]config.DogeToken(nil), source.tokens...)
		context.groups = append([]string(nil), source.groups...)
		context.candidateProfiles = append([]config.Profile(nil), source.candidateProfiles...)
		context.failoverOrder = append([]string(nil), source.failoverOrder...)
		context.profilesByID = make(map[int64]config.Profile, len(source.profilesByID))
		for id, profile := range source.profilesByID {
			context.profilesByID[id] = profile
		}
		result[category] = &context
	}
	return result
}

// cloneDogeDirectorySwitchContexts 复制目录失效快照中会跨同步保留的字段。
// 恢复判断必须使用上一次同步看到的令牌和分组状态，不能在配置写入后再读取旧快照。
func cloneDogeDirectorySwitchContexts(source map[string]*tokenSwitchContext) map[string]*tokenSwitchContext {
	result := make(map[string]*tokenSwitchContext, len(source))
	for category, item := range source {
		if item == nil {
			continue
		}
		clone := *item
		clone.tokens = append([]config.DogeToken(nil), item.tokens...)
		clone.groups = append([]string(nil), item.groups...)
		clone.failoverOrder = append([]string(nil), item.failoverOrder...)
		clone.candidateProfiles = append([]config.Profile(nil), item.candidateProfiles...)
		clone.profilesByID = make(map[int64]config.Profile, len(item.profilesByID))
		for id, profile := range item.profilesByID {
			clone.profilesByID[id] = profile
		}
		result[category] = &clone
	}
	return result
}

// recoveredDogeTokens 返回同一类别中从上一次不可用状态恢复为可用的令牌。
// 旧快照只来自目录失效上下文，且恢复必须同时满足 status、分组权限和完整密钥条件。
func recoveredDogeTokens(previous *tokenSwitchContext, cfg config.AppConfig) []config.DogeToken {
	if previous == nil {
		return nil
	}
	currentByID := make(map[int64]config.DogeToken, len(cfg.Doge.Tokens))
	for _, token := range cfg.Doge.Tokens {
		currentByID[token.ID] = token
	}
	result := make([]config.DogeToken, 0)
	seen := make(map[int64]struct{})
	for _, oldToken := range previous.tokens {
		if oldToken.ID <= 0 || dogeTokenAvailable(oldToken, previous.groups) {
			continue
		}
		profile, belongs := previous.profilesByID[oldToken.ID]
		if !belongs || profile.Category != previous.profile.Category {
			continue
		}
		current, exists := currentByID[oldToken.ID]
		if !exists || !dogeTokenSwitchable(current, cfg.Doge.Groups) {
			continue
		}
		if _, ok := seen[current.ID]; ok {
			continue
		}
		seen[current.ID] = struct{}{}
		result = append(result, current)
	}
	return result
}

// buildDogeDirectoryRecoveryNotice 生成“令牌已恢复”提示；候选使用当前类别的全部可用 Profile，
// 但排除当前仍在使用的 Profile，避免用户把切换操作提交为无变化。
func buildDogeDirectoryRecoveryNotice(cfg config.AppConfig, previous *tokenSwitchContext, recovered []config.DogeToken, candidates []config.Profile) *PublicDogeTokenSwitchPrompt {
	if previous == nil {
		return nil
	}
	// 令牌失效期间自动切换可能已经改写 ActiveProfiles；恢复提示必须以此刻实际活动项作为
	// 当前 Profile，恢复的令牌才会进入候选列表，SwitchToken 的并发状态校验也才能成立。
	currentProfile := previous.profile
	if activeID := strings.TrimSpace(cfg.ActiveProfiles[previous.profile.Category]); activeID != "" {
		if index := config.FindProfileIndex(cfg.Profiles, activeID); index >= 0 && cfg.Profiles[index].Category == previous.profile.Category {
			currentProfile = cfg.Profiles[index]
		}
	}
	current := dogeTokenForProfile(cfg, currentProfile)
	currentName := strings.TrimSpace(currentProfile.Name)
	if currentName == "" {
		currentName = current.Name
	}
	currentName = formatNonHomeProfileName(currentName, currentProfile.Source, dogeTokenDisplayGroup(current), current.GroupRatio)
	if currentName == "" {
		currentName = "当前令牌"
	}
	names := make([]string, 0, len(recovered))
	for _, token := range recovered {
		name := strings.TrimSpace(token.Name)
		if profile, ok := previous.profilesByID[token.ID]; ok && strings.TrimSpace(profile.Name) != "" {
			name = strings.TrimSpace(profile.Name)
		}
		name = formatDogeProfileName(name, dogeTokenDisplayGroup(token), token.GroupRatio)
		if name != "" {
			names = append(names, "“"+name+"”")
		}
	}
	if len(names) == 0 {
		return nil
	}
	message := fmt.Sprintf("Codex类别（%s）下%s令牌已恢复可用。", categoryDisplayName(previous.profile.Category), strings.Join(names, "、"))
	return &PublicDogeTokenSwitchPrompt{
		Key: previous.key + "|recovered", Category: previous.profile.Category, Mode: "manual", FailureKind: "directory_recovered",
		CurrentTokenID: currentProfile.RemoteTokenID, CurrentProfileID: currentProfile.ID, CurrentName: currentName,
		CurrentGroup: dogeTokenDisplayGroup(current), CurrentRatio: current.GroupRatio, Message: message,
		Candidates: publicDogeTokenSwitchCandidates(cfg, candidates, currentProfile.ID),
	}
}

func categoryDisplayName(category string) string {
	switch category {
	case config.CategoryCodex:
		return "Codex"
	case config.CategoryClaude:
		return "Claude"
	case config.CategoryGemini:
		return "Gemini"
	case config.CategoryGrok:
		return "Grok"
	case config.CategoryOpenCode:
		return "OpenCode"
	case config.CategoryOpenClaw:
		return "OpenClaw"
	case config.CategoryHermes:
		return "Hermes"
	case config.CategoryImage:
		return "生图"
	case config.CategoryOther:
		return "其他"
	default:
		return category
	}
}

// setDogeDirectorySwitchContexts 替换自动同步检测到的各类别目录失效提示。
// 提示 key 在同一活动令牌持续失效期间保持稳定，用户取消后沿用现有五分钟及持续期间抑制规则。
func (s *DesktopService) setDogeDirectorySwitchContexts(contexts map[string]*tokenSwitchContext) {
	state := s.runtime.State()
	s.switchMu.Lock()
	if s.directoryRecoveryNotices == nil {
		s.directoryRecoveryNotices = make(map[string]*PublicDogeTokenSwitchPrompt)
	}
	previousContexts := cloneDogeDirectorySwitchContexts(s.directorySwitches)
	previousKeys := make(map[string]string, len(s.directorySwitches))
	for category, context := range s.directorySwitches {
		if context != nil {
			previousKeys[category] = context.key
		}
	}
	s.directorySwitches = make(map[string]*tokenSwitchContext, len(contexts))
	changed := len(previousKeys) != len(contexts)
	for category, context := range contexts {
		if context == nil {
			continue
		}
		s.directorySwitches[category] = context
		if previousKeys[category] != context.key {
			changed = true
		}
	}
	for category := range contexts {
		delete(s.directoryRecoveryNotices, category)
	}
	s.switchMu.Unlock()
	if state != nil {
		for category, previous := range previousContexts {
			if previous == nil || previous.directoryReason != dogeDirectoryFailureUnavailable || contexts[category] != nil {
				continue
			}
			activeID := strings.TrimSpace(state.Config.ActiveProfiles[category])
			activeIndex := config.FindProfileIndex(state.Config.Profiles, activeID)
			if activeID == "" || activeIndex < 0 || state.Config.Profiles[activeIndex].Category != category || config.FindProfileIndex(state.Config.Profiles, previous.profile.ID) < 0 {
				continue
			}
			recoveredTokens := recoveredDogeTokens(previous, state.Config)
			if len(recoveredTokens) == 0 {
				continue
			}
			candidates := availableFailoverProfiles(state.Config, category)
			notice := buildDogeDirectoryRecoveryNotice(state.Config, previous, recoveredTokens, candidates)
			s.switchMu.Lock()
			if existing := s.directoryRecoveryNotices[category]; existing == nil || existing.Key != notice.Key {
				s.directoryRecoveryNotices[category] = notice
				changed = true
			}
			s.switchMu.Unlock()
		}
	}
	for _, context := range contexts {
		if context != nil {
			state = s.runtime.State()
			if state != nil && state.Config.TokenSwitch.Mode == config.TokenSwitchModeAuto &&
				directoryTriggerEnabled(state.Config.TokenSwitch, context.directoryReason) &&
				directorySwitchContextApplies(state.Config, context) {
				current := *context
				current.tokens = append([]config.DogeToken(nil), state.Config.Doge.Tokens...)
				current.groups = append([]string(nil), state.Config.Doge.Groups...)
				current.candidateProfiles = directoryFailoverCandidates(state.Config, &current)
				prompt := s.applyTokenSwitchRound(publicDogeTokenSwitchPrompt(current), true)
				s.failoverMu.Lock()
				switched, previousID := s.tryAutomaticTokenSwitch(prompt)
				s.failoverMu.Unlock()
				if switched {
					s.runtime.ResetProfileHealth(previousID)
				}
			}
		}
	}
	if changed || len(contexts) > 0 {
		s.notifyStateChanged()
	}
}

// directorySwitchContextApplies 判断目录异常是否仍对应同步前的活动令牌。
// 目录删除同步后 Profile 和启用映射已经清理，此时仅允许该 missing 快照继续完成一次手动或自动故障切换。
func directorySwitchContextApplies(cfg config.AppConfig, context *tokenSwitchContext) bool {
	if context == nil {
		return false
	}
	category := context.profile.Category
	if cfg.ActiveProfiles[category] == context.profile.ID {
		return true
	}
	return context.directoryReason == dogeDirectoryFailureMissing &&
		cfg.ActiveProfiles[category] == "" && config.FindProfileIndex(cfg.Profiles, context.profile.ID) < 0
}

// switchPromptStates 同步清理已经不再触发的提示状态，并返回当前仍允许展示的状态。
func (s *DesktopService) switchPromptStates(activeKeys map[string]struct{}) map[string]bool {
	now := time.Now()
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	visible := make(map[string]bool, len(activeKeys))
	suppressedBases := make(map[string]struct{})
	for key := range s.switchPrompts {
		promptState := s.switchPrompts[key]
		_, active := activeKeys[key]
		if !active {
			// 失败状态刚恢复时保留五分钟抑制窗口，避免短暂恢复后再次连续失败立即弹窗。
			if promptState.dismissed && now.Before(promptState.suppressedUntil) {
				suppressedBases[switchPromptBaseKey(key)] = struct{}{}
				continue
			}
			if !promptState.dismissed || !now.Before(promptState.suppressedUntil) {
				delete(s.switchPrompts, key)
			}
			continue
		}
		if promptState.dismissed {
			// 用户取消后，只要同一失败状态仍在持续，就不因五分钟到期再次打扰。
			visible[key] = true
		}
	}
	for key := range activeKeys {
		if _, ok := s.switchPrompts[key]; !ok {
			s.switchPrompts[key] = &tokenSwitchPromptState{}
		}
		if _, suppressed := suppressedBases[switchPromptBaseKey(key)]; suppressed {
			visible[key] = true
		}
	}
	return visible
}

func switchPromptBaseKey(key string) string {
	if index := strings.LastIndex(key, "|"); index >= 0 {
		return key[:index]
	}
	return key
}

// publicDogeTokenSwitchPrompt 将内部 Profile 转为不含完整密钥的前端提示；保留旧名称以兼容提醒窗口绑定。
func publicDogeTokenSwitchPrompt(context tokenSwitchContext) *PublicDogeTokenSwitchPrompt {
	currentName := strings.TrimSpace(context.profile.Name)
	if currentName == "" {
		currentName = context.token.Name
	}
	if currentName == "" {
		currentName = "当前代理 API"
	}
	currentGroup := dogeTokenDisplayGroup(context.token)
	currentName = formatNonHomeProfileName(currentName, context.profile.Source, currentGroup, context.token.GroupRatio)
	candidates := make([]PublicDogeTokenSwitchCandidate, 0)
	for _, profile := range context.candidateProfiles {
		if profile.ID == context.profile.ID {
			continue
		}
		token := context.token
		if profile.Source == config.SourceDoge {
			for _, candidateToken := range context.tokens {
				if candidateToken.ID == profile.RemoteTokenID {
					token = candidateToken
					break
				}
			}
		}
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			name = "代理 API " + profile.ID
		}
		group := ""
		ratio := float64(0)
		tokenID := int64(0)
		if profile.Source == config.SourceDoge {
			tokenID = profile.RemoteTokenID
			group = dogeTokenDisplayGroup(token)
			ratio = token.GroupRatio
		}
		name = formatNonHomeProfileName(name, profile.Source, group, ratio)
		candidates = append(candidates, PublicDogeTokenSwitchCandidate{
			TokenID: tokenID, ProfileID: profile.ID, Name: name, Source: failoverSourceLabel(profile.Source),
			Group: group, Ratio: ratio, Selectable: true,
		})
	}
	failureWindow := context.failureWindowMinutes
	if failureWindow <= 0 {
		failureWindow = config.DefaultUpstreamFailureWindowMinutes
	}
	failureMessage := fmt.Sprintf("当前代理 API“%s”在 %d 分钟内出现 %d 次上游异常，是否切换到列表中的下一个可用项？", currentName, failureWindow, context.failureCount)
	if context.failureKind == "auth" {
		failureMessage = fmt.Sprintf("当前代理 API“%s”连续 %d 次返回 HTTP %d，是否切换到列表中的下一个可用项？", currentName, context.failureCount, context.failureStatus)
	} else if context.failureKind == "directory" {
		if context.directoryReason == dogeDirectoryFailureMissing {
			failureMessage = fmt.Sprintf("当前令牌“%s”已从最新令牌目录中消失，是否切换到列表中的下一个可用项？", currentName)
		} else {
			failureMessage = fmt.Sprintf("当前令牌“%s”在最新令牌目录中已失效，是否切换到列表中的下一个可用项？", currentName)
		}
	}
	return &PublicDogeTokenSwitchPrompt{
		Key: context.key, Category: context.profile.Category, Mode: "manual", FailureKind: context.failureKind,
		FailureCount: context.failureCount, FailureStatus: context.failureStatus, FailureWindowMinutes: failureWindow, CurrentTokenID: context.token.ID, CurrentProfileID: context.profile.ID,
		CurrentName: currentName, CurrentGroup: currentGroup, CurrentRatio: context.token.GroupRatio,
		Message: failureMessage, Candidates: candidates,
	}
}

func publicDogeTokenSwitchCandidates(cfg config.AppConfig, profiles []config.Profile, currentID string) []PublicDogeTokenSwitchCandidate {
	candidates := make([]PublicDogeTokenSwitchCandidate, 0, len(profiles))
	for _, profile := range profiles {
		if profile.ID == currentID {
			continue
		}
		token := dogeTokenForProfile(cfg, profile)
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			name = "代理 API " + profile.ID
		}
		group := ""
		ratio := float64(0)
		tokenID := int64(0)
		if profile.Source == config.SourceDoge {
			tokenID = profile.RemoteTokenID
			group = dogeTokenDisplayGroup(token)
			ratio = token.GroupRatio
		}
		name = formatNonHomeProfileName(name, profile.Source, group, ratio)
		candidates = append(candidates, PublicDogeTokenSwitchCandidate{
			TokenID: tokenID, ProfileID: profile.ID, Name: name, Source: failoverSourceLabel(profile.Source),
			Group: group, Ratio: ratio, Selectable: true,
		})
	}
	return candidates
}

// formatNonHomeProfileName 统一托盘、提醒窗口和统计页等非主页位置的 Profile 名称。
// 主界面与编辑器仍使用原始名称；自定义 API 没有二狗子分组和倍率，只追加固定来源说明。
func formatNonHomeProfileName(name, source, group string, ratio float64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "未命名令牌"
	}
	if source == config.SourceCustom {
		return fmt.Sprintf("%s（自定义 API）", name)
	}
	return formatDogeProfileName(name, group, ratio)
}

// formatDogeProfileName 统一非主页位置的二狗子令牌名称；主界面列表仍分别展示名称、分组和倍率标签。
func formatDogeProfileName(name, group string, ratio float64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "未命名令牌"
	}
	group = strings.TrimSpace(group)
	if group == "" {
		return name
	}
	if ratio > 0 {
		return fmt.Sprintf("%s (%s·%s)", name, group, formatDogeRatio(ratio))
	}
	return fmt.Sprintf("%s (%s)", name, group)
}

func publicDogeNotifications(connection config.DogeConnection, walletUSD float64, subscriptions []PublicDogeSubscription, syncing bool) PublicDogeNotifications {
	readIDs := make(map[int64]struct{}, len(connection.Notifications.ReadAnnouncementIDs))
	for _, id := range connection.Notifications.ReadAnnouncementIDs {
		readIDs[id] = struct{}{}
	}
	dismissed := make(map[string]struct{}, len(connection.Notifications.DismissedAlertKeys))
	for _, key := range connection.Notifications.DismissedAlertKeys {
		dismissed[key] = struct{}{}
	}
	balanceRecords := make(map[int64]config.DogeBalanceAlertRecord, len(connection.Notifications.BalanceAlertRecords))
	for _, record := range connection.Notifications.BalanceAlertRecords {
		if record.AccountID > 0 {
			balanceRecords[record.AccountID] = record
		}
	}
	subscriptionRecords := make(map[int64]config.DogeSubscriptionAlertRecord, len(connection.Notifications.SubscriptionAlertRecords))
	for _, record := range connection.Notifications.SubscriptionAlertRecords {
		if record.SubscriptionID > 0 {
			subscriptionRecords[record.SubscriptionID] = record
		}
	}
	publicAnnouncements := make([]PublicDogeAnnouncement, 0, len(connection.Notifications.Announcements))
	unread := 0
	for _, announcement := range connection.Notifications.Announcements {
		_, read := readIDs[announcement.ID]
		if !read {
			unread++
		}
		publicAnnouncements = append(publicAnnouncements, PublicDogeAnnouncement{
			ID: announcement.ID, Content: announcement.Content, Extra: announcement.Extra,
			PublishDate: announcement.PublishDate, Type: announcement.Type, Read: read,
		})
	}
	alerts := make([]PublicDogeAlert, 0)
	if connection.Notifications.Initialized && connection.Notifications.AnnouncementsEnabled {
		for _, announcement := range connection.Notifications.Announcements {
			key := announcementAlertKey(announcement.ID)
			if _, read := readIDs[announcement.ID]; read {
				continue
			}
			if _, ok := dismissed[key]; ok {
				continue
			}
			alerts = append(alerts, PublicDogeAlert{Kind: NotificationKindAnnouncement, Key: key, Title: "新的系统公告", Message: "平台发布了新的公告", AnnouncementID: announcement.ID})
		}
	}
	if connection.Notifications.BalanceAlertEnabled && connection.Account.ID > 0 && walletUSD < connection.Notifications.BalanceAlertThresholdUSD {
		key := balanceAlertKey(connection.Account.ID)
		record, tracked := balanceRecords[connection.Account.ID]
		if !tracked {
			_, tracked = dismissed[key]
			record.Acknowledged = tracked
		}
		if tracked && !record.Acknowledged {
			alerts = append(alerts, PublicDogeAlert{Kind: NotificationKindBalance, Key: key, Title: "余额提醒", Message: fmt.Sprintf("钱包余额仅剩 %s", formatDogeUSDValue(walletUSD)), AmountUSD: walletUSD})
		}
	}
	for _, subscription := range subscriptions {
		if !connection.Notifications.SubscriptionAlertEnabled {
			continue
		}
		key := subscriptionAlertKey(subscription.ID)
		record, tracked := subscriptionRecords[subscription.ID]
		if !tracked {
			_, tracked = dismissed[key]
			record.Acknowledged = tracked
		}
		label := subscription.PlanTitle
		if strings.TrimSpace(label) == "" {
			label = fmt.Sprintf("套餐 %d", subscription.PlanID)
		}
		if tracked && !record.Acknowledged {
			state := record.State
			if state == "" {
				state = subscriptionAlertStateLowBalance
			}
			title := "套餐余额提醒"
			message := fmt.Sprintf("%s 剩余 %s", label, formatDogeUSDValue(subscription.RemainingUSD))
			if state == subscriptionAlertStateExpiringSoon {
				hours := time.Until(time.Unix(subscription.EndTime, 0)).Hours()
				title = "套餐即将过期"
				message = fmt.Sprintf("%s 将在 %s 内过期，当前剩余 %s，请及时使用。", label, formatDogeDuration(hours), formatDogeUSDValue(subscription.RemainingUSD))
			} else if state == subscriptionAlertStateExpired {
				title = "套餐已过期"
				message = fmt.Sprintf("%s 已过期，剩余金额 %s。", label, formatDogeUSDValue(subscription.RemainingUSD))
			}
			alerts = append(alerts, PublicDogeAlert{Kind: NotificationKindSubscription, Key: key, Title: title, Message: message, AmountUSD: subscription.RemainingUSD})
		}
	}
	lastSyncAt := ""
	if !connection.Notifications.LastAnnouncementSyncAt.IsZero() {
		lastSyncAt = connection.Notifications.LastAnnouncementSyncAt.Format(time.RFC3339)
	}
	return PublicDogeNotifications{
		Initialized:   connection.Notifications.Initialized,
		Enabled:       connection.Notifications.AnnouncementsEnabled,
		CurrentNotice: connection.Notifications.CurrentNotice,
		Announcements: publicAnnouncements,
		UnreadCount:   unread,
		Alerts:        alerts,
		LastSyncAt:    lastSyncAt,
		LastSyncError: connection.Notifications.LastAnnouncementSyncError,
		Syncing:       syncing,
	}
}

// dogeQuotaToUSD 使用二狗子当前实例的额度换算规则将原始 quota 转为美元。
// 该规则来自当前接口样本：500000 quota = 1 美元；配置仍保留原始整数额度。
func dogeQuotaToUSD(quota int64) float64 {
	return float64(quota) / 500000
}

func formatDogeUSDValue(value float64) string {
	return fmt.Sprintf("$%.2f", value)
}

func formatDogeDuration(hours float64) string {
	if hours < 1 {
		minutes := int(math.Ceil(hours * 60))
		if minutes < 1 {
			minutes = 1
		}
		return fmt.Sprintf("%d 分钟", minutes)
	}
	return fmt.Sprintf("%.1f 小时", hours)
}

func balanceAlertKey(userID int64) string {
	return fmt.Sprintf("balance:%d", userID)
}

func subscriptionAlertKey(subscriptionID int64) string {
	return fmt.Sprintf("subscription:%d", subscriptionID)
}

func subscriptionExpiredAlertKey(subscriptionID int64) string {
	return fmt.Sprintf("subscription-expired:%d", subscriptionID)
}

func announcementAlertKey(announcementID int64) string {
	return fmt.Sprintf("announcement:%d", announcementID)
}

func (s *DesktopService) ReorderProfiles(ids []string) error {
	return s.updateConfig(func(cfg *config.AppConfig) error {
		ordered, err := config.OrderProfiles(cfg.Profiles, ids)
		if err != nil {
			return err
		}
		cfg.Profiles = ordered
		return nil
	})
}

// ReorderDogeTokens 按二狗子远端令牌 ID 保存主页顺序；名称、分组和 API 密钥只用于展示或访问，
// 不参与令牌身份定位。
func (s *DesktopService) ReorderDogeTokens(orderKeys []string) error {
	return s.updateConfig(func(cfg *config.AppConfig) error {
		known := make(map[string]struct{}, len(cfg.Doge.Tokens))
		for _, token := range cfg.Doge.Tokens {
			key := dogeTokenOrderKey(token)
			if key == "" {
				continue
			}
			known[key] = struct{}{}
		}
		seen := make(map[string]struct{}, len(orderKeys))
		next := make([]string, 0, len(known))
		for _, key := range orderKeys {
			key = strings.TrimSpace(key)
			if key == "" {
				return errors.New("二狗子令牌排序 ID 不能为空")
			}
			if _, ok := known[key]; !ok {
				return errors.New("二狗子令牌排序包含未知 ID")
			}
			if _, ok := seen[key]; ok {
				return errors.New("二狗子令牌排序包含重复 ID")
			}
			seen[key] = struct{}{}
			next = append(next, key)
		}
		for _, token := range cfg.Doge.Tokens {
			key := dogeTokenOrderKey(token)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				next = append(next, key)
			}
		}
		cfg.Doge.TokenOrder = next
		cfg.Doge.Tokens = orderDogeTokens(next, cfg.Doge.Tokens)
		return nil
	})
}

// SetDogeTokenCategories 校验整批选择后，将待导入令牌一次性创建为本地 Profile。
// 新 Profile 按本批选择顺序追加到所属类别末尾；整批任一项无效时不保存部分结果。
func (s *DesktopService) SetDogeTokenCategories(assignments []DogeTokenCategoryInput) error {
	if len(assignments) == 0 {
		return errors.New("至少选择一个二狗子令牌类别")
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		byID := make(map[int64]int, len(cfg.Doge.Tokens))
		for index := range cfg.Doge.Tokens {
			byID[cfg.Doge.Tokens[index].ID] = index
		}
		imported := make(map[int64]struct{})
		for _, profile := range cfg.Profiles {
			if profile.Source == config.SourceDoge && profile.RemoteTokenID > 0 {
				imported[profile.RemoteTokenID] = struct{}{}
			}
		}
		seen := make(map[int64]struct{}, len(assignments))
		for _, assignment := range assignments {
			if assignment.ID <= 0 {
				return errors.New("二狗子令牌 ID 无效")
			}
			if _, ok := seen[assignment.ID]; ok {
				return fmt.Errorf("二狗子令牌 %d 重复选择类别", assignment.ID)
			}
			seen[assignment.ID] = struct{}{}
			if !config.IsCategory(assignment.Category) {
				return fmt.Errorf("二狗子令牌 %d 的存放类别无效", assignment.ID)
			}
			index, ok := byID[assignment.ID]
			if !ok {
				return fmt.Errorf("二狗子令牌 %d 不存在，请先刷新目录", assignment.ID)
			}
			if _, exists := imported[assignment.ID]; exists {
				return fmt.Errorf("二狗子令牌 %d 已导入，不能通过同步弹窗修改类别", assignment.ID)
			}
			remote := cfg.Doge.Tokens[index]
			remote.Key = normalizeDogeAPIKey(remote.Key)
			if !isCompleteDogeAPIKey(remote.Key) {
				return fmt.Errorf("二狗子令牌 %d 缺少完整 API 密钥，请先手动同步", assignment.ID)
			}
		}
		for _, assignment := range assignments {
			index := byID[assignment.ID]
			cfg.Doge.Tokens[index].Category = assignment.Category
			cfg.Profiles = append(cfg.Profiles, newDogeProfile(cfg, cfg.Doge.Tokens[index], assignment.Category))
		}
		cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
		return nil
	}); err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
}

// newDogeProfile 只负责把已经通过类别和完整密钥校验的目录令牌转换为本地 Profile。
// 调用方必须在同一个配置事务中保存，避免令牌类别与 Profile 顺序分离。
func newDogeProfile(cfg *config.AppConfig, remote config.DogeToken, category string) config.Profile {
	name := strings.TrimSpace(remote.Name)
	if name == "" {
		name = "二狗子令牌 " + strconv.FormatInt(remote.ID, 10)
	}
	note := strings.TrimSpace(remote.Note)
	if note == "" {
		note = dogeTokenNote(remote)
	}
	return config.Profile{
		ID: config.NewProfileID(), Source: config.SourceDoge, Category: category, Name: name,
		BaseURL: strings.TrimRight(cfg.Doge.BaseURL, "/") + "/v1", APIKey: normalizeDogeAPIKey(remote.Key),
		Note: note, RemoteTokenID: remote.ID,
	}
}

func (s *DesktopService) ClearUsage() error {
	if err := s.runtime.ClearUsage(); err != nil {
		return err
	}
	s.notifyStateChanged()
	return nil
}

// SaveProfile 新增或更新一个本地 Profile，并规范化其类别故障顺序。
// 保存成功后重新评估正在进行的自动轮次，使新 Profile 可以成为目录异常后的实时候选。
func (s *DesktopService) SaveProfile(input ProfileInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.Note = strings.TrimSpace(input.Note)
	input.DefaultModel = strings.TrimSpace(input.DefaultModel)
	if input.Headers == nil {
		input.Headers = map[string]string{}
	}
	modelsOmitted := input.Models == nil
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, input.ID)
		var previous config.Profile
		if index >= 0 {
			previous = cfg.Profiles[index]
			if modelsOmitted {
				input.Models = append([]ModelInput(nil), configModelsToInput(previous.Models)...)
			}
			if modelsOmitted && input.DefaultModel == "" && previous.DefaultModel != "" {
				input.DefaultModel = previous.DefaultModel
			}
		}
		if index >= 0 && previous.Source == config.SourceDoge {
			if input.Source != config.SourceDoge {
				return errors.New("二狗子代理 API 来源不能修改")
			}
			// 二狗子密钥由远端令牌接口维护；后端再次校验，避免绕过前端只读控件修改。
			if normalizeDogeAPIKey(input.APIKey) != normalizeDogeAPIKey(previous.APIKey) {
				return errors.New("二狗子 API 密钥由远端管理，不能修改")
			}
			input.APIKey = normalizeDogeAPIKey(previous.APIKey)
		}
		profile := config.Profile{
			ID: input.ID, Source: input.Source, Category: input.Category, Name: input.Name, BaseURL: input.BaseURL,
			APIKey: input.APIKey, Note: input.Note, Headers: input.Headers, Models: modelInputsToConfig(input.Models), DefaultModel: input.DefaultModel,
		}
		if index >= 0 {
			profile.RemoteTokenID = previous.RemoteTokenID
			profile.SkipAutoSwitch = previous.SkipAutoSwitch
		}
		if index < 0 {
			profile.ID = config.NewProfileID()
		}
		if profile.APIKey == "" {
			return errors.New("API 密钥不能为空")
		}
		if len([]rune(profile.Note)) > 160 {
			return errors.New("备注说明不能超过 160 个字符")
		}
		if err := config.ValidateProfile(profile); err != nil {
			return err
		}
		if index >= 0 {
			cfg.Profiles[index] = profile
			if profile.Source == config.SourceDoge && profile.RemoteTokenID > 0 {
				for tokenIndex := range cfg.Doge.Tokens {
					if cfg.Doge.Tokens[tokenIndex].ID == profile.RemoteTokenID {
						cfg.Doge.Tokens[tokenIndex].Category = profile.Category
						break
					}
				}
			}
			if previous.Category != profile.Category && cfg.ActiveProfiles[previous.Category] == profile.ID {
				delete(cfg.ActiveProfiles, previous.Category)
			}
		} else {
			cfg.Profiles = append(cfg.Profiles, profile)
		}
		cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
		return nil
	}); err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
}

// SetProfileAutoSwitch 设置单个 Profile 是否参加自动故障切换；手动提示模式仍可选择该 Profile。
func (s *DesktopService) SetProfileAutoSwitch(id string, enabled bool) error {
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, strings.TrimSpace(id))
		if index < 0 {
			return errors.New("代理 API 不存在")
		}
		cfg.Profiles[index].SkipAutoSwitch = !enabled
		return nil
	}); err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
}

func (s *DesktopService) DeleteProfile(id string) error {
	return s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, id)
		if index < 0 {
			return errors.New("代理 API 不存在")
		}
		category := cfg.Profiles[index].Category
		cfg.Profiles = append(cfg.Profiles[:index], cfg.Profiles[index+1:]...)
		if cfg.ActiveProfiles[category] == id {
			delete(cfg.ActiveProfiles, category)
		}
		cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
		return nil
	})
}

// ActivateProfile 启用指定 Profile；二狗子来源必须仍存在于最新目录且当前分组可用。
// 成功后清理原活动 Profile 的失败统计，新请求立即读取新的运行时快照。
func (s *DesktopService) ActivateProfile(id string) error {
	state := s.runtime.State()
	previousID := ""
	if state != nil {
		if index := config.FindProfileIndex(state.Config.Profiles, id); index >= 0 {
			previousID = state.Config.ActiveProfiles[state.Config.Profiles[index].Category]
		}
	}
	err := s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, id)
		if index >= 0 {
			profile := cfg.Profiles[index]
			if profile.Source == config.SourceDoge {
				if profile.RemoteTokenID <= 0 {
					return errors.New("二狗子令牌缺少有效的远端目录 ID，不能启用")
				}
				found := false
				for _, token := range cfg.Doge.Tokens {
					if token.ID != profile.RemoteTokenID {
						continue
					}
					found = true
					if !dogeTokenAvailable(token, cfg.Doge.Groups) {
						return errors.New("二狗子令牌当前分组不可用，不能启用")
					}
					break
				}
				if !found {
					return errors.New("二狗子令牌已不在最新目录中，请先同步")
				}
			}
			if cfg.ActiveProfiles == nil {
				cfg.ActiveProfiles = map[string]string{}
			}
			cfg.ActiveProfiles[profile.Category] = id
			return nil
		}
		return errors.New("代理 API 不存在")
	})
	if err != nil {
		return err
	}
	if previousID != "" && previousID != id {
		s.runtime.ResetProfileHealth(previousID)
	}
	return nil
}

func directoryTriggerEnabled(settings config.TokenSwitchSettings, reason string) bool {
	if reason == dogeDirectoryFailureMissing {
		return settings.TriggerDirectoryMissing
	}
	return settings.TriggerDirectoryInvalid
}

func (s *DesktopService) failoverCandidates(category, currentID string, loop bool) []config.Profile {
	state := s.runtime.State()
	if state == nil {
		return nil
	}
	return failoverCandidatesFromConfig(state.Config, category, currentID, loop)
}

// failoverCandidatesFromConfig 按给定配置快照计算一个类别的后续候选。
// 目录同步在删除当前 Profile 前调用它，既保留原列表位置，又使用最新目录过滤同时消失或失效的候选。
func failoverCandidatesFromConfig(cfg config.AppConfig, category, currentID string, loop bool) []config.Profile {
	ordered := config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)[category]
	byID := make(map[string]config.Profile, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		if profile.Category == category {
			byID[profile.ID] = profile
		}
	}
	start := -1
	for index, id := range ordered {
		if id == currentID {
			start = index
			break
		}
	}
	if start < 0 {
		start = -1
	}
	limit := len(ordered)
	if !loop && start >= 0 {
		limit -= start + 1
	}
	result := make([]config.Profile, 0, len(ordered))
	for step := 1; step <= limit; step++ {
		index := start + step
		if loop {
			index %= len(ordered)
		}
		if index < 0 || index >= len(ordered) || (loop && index == start) {
			break
		}
		profile, ok := byID[ordered[index]]
		if !ok || !failoverProfileAvailable(cfg, profile) {
			continue
		}
		if cfg.TokenSwitch.Mode == config.TokenSwitchModeAuto && profile.SkipAutoSwitch {
			continue
		}
		result = append(result, profile)
	}
	return result
}

// directoryFailoverCandidates 使用当前配置重算目录异常候选，仅从同步前顺序保留已删除当前项的位置。
// 模式、循环、跳过状态、新增 Profile 和最新二狗子目录都以调用时快照为准，不能沿用发生异常时的候选列表。
func directoryFailoverCandidates(cfg config.AppConfig, context *tokenSwitchContext) []config.Profile {
	if context == nil {
		return nil
	}
	candidateConfig := config.Clone(cfg)
	category := context.profile.Category
	currentID := context.profile.ID
	if context.directoryReason == dogeDirectoryFailureMissing {
		if config.FindProfileIndex(candidateConfig.Profiles, currentID) < 0 {
			candidateConfig.Profiles = append(candidateConfig.Profiles, context.profile)
		}
		currentOrder := config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)[category]
		anchoredOrder := insertMissingFailoverAnchor(currentOrder, context.failoverOrder, currentID)
		if candidateConfig.FailoverOrder == nil {
			candidateConfig.FailoverOrder = make(map[string][]string)
		}
		candidateConfig.FailoverOrder[category] = anchoredOrder
	}
	return failoverCandidatesFromConfig(candidateConfig, category, currentID, candidateConfig.TokenSwitch.Loop)
}

// insertMissingFailoverAnchor 把已删除当前项放回最新可见顺序中的原相对位置。
// 优先使用仍存在的前一项，其次使用后一项；两侧都不存在时按原索引落位，使列表首尾语义保持稳定。
func insertMissingFailoverAnchor(currentOrder, previousOrder []string, currentID string) []string {
	result := append([]string(nil), currentOrder...)
	for _, id := range result {
		if id == currentID {
			return result
		}
	}
	previousIndex := -1
	for index, id := range previousOrder {
		if id == currentID {
			previousIndex = index
			break
		}
	}
	positions := make(map[string]int, len(result))
	for index, id := range result {
		positions[id] = index
	}
	insertAt := -1
	if previousIndex >= 0 {
		for index := previousIndex - 1; index >= 0; index-- {
			if position, ok := positions[previousOrder[index]]; ok {
				insertAt = position + 1
				break
			}
		}
		if insertAt < 0 {
			for index := previousIndex + 1; index < len(previousOrder); index++ {
				if position, ok := positions[previousOrder[index]]; ok {
					insertAt = position
					break
				}
			}
		}
	}
	if insertAt < 0 {
		insertAt = previousIndex
		if insertAt < 0 || insertAt > len(result) {
			insertAt = len(result)
		}
	}
	result = append(result, "")
	copy(result[insertAt+1:], result[insertAt:])
	result[insertAt] = currentID
	return result
}

func availableFailoverProfiles(cfg config.AppConfig, category string) []config.Profile {
	order := config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)[category]
	byID := make(map[string]config.Profile, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		if profile.Category == category {
			byID[profile.ID] = profile
		}
	}
	result := make([]config.Profile, 0, len(order))
	for _, id := range order {
		profile, ok := byID[id]
		if ok && failoverProfileAvailable(cfg, profile) {
			result = append(result, profile)
		}
	}
	return result
}

func failoverProfileAvailable(cfg config.AppConfig, profile config.Profile) bool {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.APIKey) == "" {
		return false
	}
	if profile.Source != config.SourceDoge {
		return true
	}
	if profile.RemoteTokenID <= 0 {
		return false
	}
	for _, token := range cfg.Doge.Tokens {
		if token.ID == profile.RemoteTokenID {
			return dogeTokenSwitchable(token, cfg.Doge.Groups)
		}
	}
	return false
}

func dogeTokenForProfile(cfg config.AppConfig, profile config.Profile) config.DogeToken {
	if profile.Source != config.SourceDoge || profile.RemoteTokenID <= 0 {
		return config.DogeToken{}
	}
	for _, token := range cfg.Doge.Tokens {
		if token.ID == profile.RemoteTokenID {
			return token
		}
	}
	return config.DogeToken{ID: profile.RemoteTokenID, Name: profile.Name, Category: profile.Category}
}

func failoverSourceLabel(source string) string {
	if source == config.SourceDoge {
		return "二狗子 API"
	}
	return "自定义 API"
}

func dogeFailoverProfileID(tokenID int64) string {
	return fmt.Sprintf("doge-token:%d", tokenID)
}

func dogeTokenIDFromFailoverProfileID(profileID string) (int64, bool) {
	if !strings.HasPrefix(profileID, "doge-token:") {
		return 0, false
	}
	tokenID, err := strconv.ParseInt(strings.TrimPrefix(profileID, "doge-token:"), 10, 64)
	return tokenID, err == nil && tokenID > 0
}

// ReorderFailoverProfiles 保存指定类别的统一 Profile 顺序；来源不同的 Profile 可以在同一列表中相邻排列。
// 前端只提交当前视图中的 Profile ID，后端保留未显示项并拒绝跨类别或未知 ID。
func (s *DesktopService) ReorderFailoverProfiles(category string, ids []string) error {
	category = strings.TrimSpace(category)
	if !config.IsCategory(category) {
		return errors.New("API 类别无效")
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		known := make(map[string]struct{})
		for _, profile := range cfg.Profiles {
			if profile.Category == category {
				known[profile.ID] = struct{}{}
			}
		}
		seen := make(map[string]struct{}, len(ids))
		next := make([]string, 0, len(known))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if _, ok := known[id]; !ok {
				return errors.New("令牌切换顺序包含未知或跨类别 Profile")
			}
			if _, ok := seen[id]; ok {
				return errors.New("令牌切换顺序包含重复 Profile")
			}
			seen[id] = struct{}{}
			next = append(next, id)
		}
		for _, id := range config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)[category] {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			next = append(next, id)
		}
		if cfg.FailoverOrder == nil {
			cfg.FailoverOrder = map[string][]string{}
		}
		cfg.FailoverOrder[category] = next
		return nil
	}); err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
}

// DismissDogeTokenSwitch 在当前失败状态持续期间抑制令牌切换提示，并在失败恢复后的五分钟内继续抑制。
// 抑制状态仅保存在内存中，不修改便携配置；失败状态恢复后会自动清理。
func (s *DesktopService) DismissDogeTokenSwitch(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("令牌切换提示已失效")
	}
	s.switchMu.Lock()
	promptState, ok := s.switchPrompts[key]
	autoNotice := false
	recoveryNotice := false
	for category, notice := range s.autoSwitchNotices {
		if notice != nil && notice.Key == key {
			delete(s.autoSwitchNotices, category)
			autoNotice = true
			break
		}
	}
	for category, notice := range s.directoryRecoveryNotices {
		if notice != nil && notice.Key == key {
			delete(s.directoryRecoveryNotices, category)
			recoveryNotice = true
			break
		}
	}
	if ok {
		promptState.dismissed = true
		promptState.suppressedUntil = time.Now().Add(5 * time.Minute)
	}
	s.switchMu.Unlock()
	if !ok && !autoNotice && !recoveryNotice {
		return errors.New("令牌切换提示已失效")
	}
	s.notifyStateChanged()
	return nil
}

// SwitchDogeToken 保留旧绑定入口；实际切换已提升为所有来源共用的 Profile 切换。
func (s *DesktopService) SwitchDogeToken(key string, tokenID int64) error {
	state := s.runtime.State()
	if state == nil || tokenID <= 0 {
		return errors.New("令牌切换参数无效")
	}
	for _, profile := range state.Config.Profiles {
		if profile.Source == config.SourceDoge && profile.RemoteTokenID == tokenID {
			return s.SwitchToken(key, profile.ID)
		}
	}
	return s.SwitchToken(key, dogeFailoverProfileID(tokenID))
}

// SwitchToken 在服务端重新校验提示、类别、顺序和可用状态后启用候选 Profile。
// 前端只提交运行时提示 key 与 Profile ID，不能借此切换到其他类别或不可用 Profile。
func (s *DesktopService) SwitchToken(key, profileID string) error {
	key = strings.TrimSpace(key)
	profileID = strings.TrimSpace(profileID)
	if key == "" || profileID == "" {
		return errors.New("令牌切换参数无效")
	}
	s.failoverMu.Lock()
	var prompt *PublicDogeTokenSwitchPrompt
	for _, candidatePrompt := range s.buildTokenSwitchPrompts() {
		if candidatePrompt != nil && candidatePrompt.Key == key {
			prompt = candidatePrompt
			break
		}
	}
	if prompt == nil {
		s.failoverMu.Unlock()
		return errors.New("令牌切换提示已失效，请重新等待下一次异常")
	}
	found := false
	for _, candidate := range prompt.Candidates {
		if candidate.ProfileID == profileID && candidate.Selectable {
			found = true
			break
		}
	}
	if !found {
		s.failoverMu.Unlock()
		return errors.New("候选 Profile 当前不可用")
	}
	if err := s.switchProfile(prompt.Category, prompt.CurrentProfileID, profileID, tokenSwitchCurrentWasRemoved(s.runtime.State(), prompt)); err != nil {
		s.failoverMu.Unlock()
		return err
	}
	s.clearSwitchPrompt(key)
	s.failoverMu.Unlock()
	s.runtime.ResetProfileHealth(prompt.CurrentProfileID)
	s.notifyStateChanged()
	return nil
}

// tokenSwitchCurrentWasRemoved 只为目录删除产生的有效提示放宽当前 Profile 校验。
// 普通健康错误和目录禁用仍要求 ActiveProfiles 精确匹配，不能借过期提示启用任意候选。
func tokenSwitchCurrentWasRemoved(state *relay.State, prompt *PublicDogeTokenSwitchPrompt) bool {
	return state != nil && prompt != nil && prompt.FailureKind == "directory" &&
		state.Config.ActiveProfiles[prompt.Category] == "" && config.FindProfileIndex(state.Config.Profiles, prompt.CurrentProfileID) < 0
}

func (s *DesktopService) switchProfile(category, currentID, candidateID string, allowRemovedCurrent bool) error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	currentMatches := state.Config.ActiveProfiles[category] == currentID
	currentWasRemoved := allowRemovedCurrent && state.Config.ActiveProfiles[category] == "" && config.FindProfileIndex(state.Config.Profiles, currentID) < 0
	if !currentMatches && !currentWasRemoved {
		return errors.New("当前代理 API 已发生变化，请重新等待下一次异常")
	}
	index := config.FindProfileIndex(state.Config.Profiles, candidateID)
	candidateIsRemote := false
	if index < 0 {
		remoteID, ok := dogeTokenIDFromFailoverProfileID(candidateID)
		if !ok {
			return errors.New("候选 Profile 不存在或类别不匹配")
		}
		for _, token := range state.Config.Doge.Tokens {
			if token.ID == remoteID && token.Category == category && dogeTokenSwitchable(token, state.Config.Doge.Groups) {
				candidateIsRemote = true
				break
			}
		}
		if !candidateIsRemote {
			return errors.New("候选令牌当前不可用")
		}
		if err := s.prepareDogeTokenProfile(remoteID, false); err != nil {
			return err
		}
		state = s.runtime.State()
		for candidateIndex, profile := range state.Config.Profiles {
			if profile.Source == config.SourceDoge && profile.RemoteTokenID == remoteID && profile.Category == category {
				index = candidateIndex
				candidateID = profile.ID
				break
			}
		}
	}
	if index < 0 || state.Config.Profiles[index].Category != category {
		return errors.New("候选 Profile 不存在或类别不匹配")
	}
	candidate := state.Config.Profiles[index]
	if !failoverProfileAvailable(state.Config, candidate) {
		return errors.New("候选 Profile 当前不可用")
	}
	clientEntry := state.Config.ClientConfigs[category]
	if clientconfig.Supports(category) && !clientEntry.SkipConfigReplacement {
		if err := clientconfig.Configure(state.Config, category, candidate.ID); err != nil {
			return fmt.Errorf("更新客户端配置失败: %w", err)
		}
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		currentMatches := cfg.ActiveProfiles[category] == currentID
		currentWasRemoved := allowRemovedCurrent && cfg.ActiveProfiles[category] == "" && config.FindProfileIndex(cfg.Profiles, currentID) < 0
		if !currentMatches && !currentWasRemoved {
			return errors.New("当前代理 API 已发生变化，请重新等待下一次异常")
		}
		cfg.ActiveProfiles[category] = candidate.ID
		return nil
	})
}

func (s *DesktopService) SetNetwork(input network.Settings) error {
	input.Mode = strings.TrimSpace(input.Mode)
	input.ProxyURL = strings.TrimSpace(input.ProxyURL)
	return s.updateConfig(func(cfg *config.AppConfig) error {
		if err := network.Validate(input, cfg.ProxyPort); err != nil {
			return err
		}
		cfg.Network = input
		return nil
	})
}

// SetProxyPort 校验并热切换本地代理监听端口；新端口无法绑定时不修改配置和现有监听。
// 端口范围为 TCP 的 1-65535；成功后新请求地址立即使用新端口，已有连接由旧服务优雅退出。
func (s *DesktopService) SetProxyPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("监听端口必须是 1 到 65535 之间的整数")
	}
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	if state.Config.ProxyPort == port {
		return nil
	}
	listener, server, err := s.prepareProxyListener(port, state.Config.ListenOnAllInterfaces)
	if err != nil {
		return err
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.ProxyPort = port
		return nil
	}); err != nil {
		_ = listener.Close()
		return err
	}
	if !s.installProxyListener(server, listener) {
		_ = listener.Close()
		return nil
	}
	return nil
}

// SetProxyListenAllInterfaces 切换透明代理是否监听所有 IPv4 网卡；绑定失败时保留原配置和监听。
// 开启后 WSL2 可通过 Windows 主机地址访问，但所有请求仍须通过本地访问令牌认证。
func (s *DesktopService) SetProxyListenAllInterfaces(enabled bool) error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("代理尚未初始化")
	}
	if state.Config.ListenOnAllInterfaces == enabled {
		return nil
	}
	listener, server, err := s.prepareProxyListener(state.Config.ProxyPort, enabled)
	if err != nil {
		return err
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.ListenOnAllInterfaces = enabled
		return nil
	}); err != nil {
		_ = listener.Close()
		return err
	}
	if !s.installProxyListener(server, listener) {
		_ = listener.Close()
	}
	return nil
}

func (s *DesktopService) SetPreferences(input config.Preferences) error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	previous := state.Config.Preferences.LaunchAtStartup
	if err := platform.SetLaunchAtStartup(input.LaunchAtStartup); err != nil {
		return fmt.Errorf("更新开机启动失败: %w", err)
	}
	_, err := s.runtime.UpdateConfig(func(cfg *config.AppConfig) error {
		cfg.Preferences = input
		return nil
	})
	if err != nil {
		_ = platform.SetLaunchAtStartup(previous)
		return err
	}
	s.notifyStateChanged()
	return nil
}

// SetTaskNotification 保存独立任务完成通知的完整访问 URL 和重试设置。
// 修改只影响后台 watcher；它不写入 Codex 的 config.toml、hooks.json 或系统代理。
func (s *DesktopService) SetTaskNotification(input config.TaskNotification) error {
	input.WebhookURL = strings.TrimSpace(input.WebhookURL)
	// 设置页提交代表用户已明确选择事件范围，允许其关闭全部事件而不被默认值覆盖。
	input.EventsInitialized = true
	input = config.NormalizeTaskNotification(input)
	if err := config.ValidateTaskNotification(input); err != nil {
		return err
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.TaskNotification = input
		return nil
	})
}

// TestTaskNotification 由用户在设置页确认后测试当前 Webhook。测试请求不包含
// rollout、任务标识、路径、prompt 或最终回复，失败不会改变 pending/outbox 队列。
func (s *DesktopService) TestTaskNotification() error {
	if s.taskNotifier == nil {
		return errors.New("任务通知服务尚未初始化")
	}
	if err := s.taskNotifier.Test(context.Background()); err != nil {
		return fmt.Errorf("测试任务通知失败: %w", err)
	}
	return nil
}

// SetTokenSwitchSettings 保存所有来源共用的故障触发、阈值和候选循环策略。
// 关闭或重新开启某个错误类型会清理其旧统计；阈值、窗口和模式变化则基于仍有效的其他统计重新评估。
func (s *DesktopService) SetTokenSwitchSettings(input config.TokenSwitchSettings) error {
	if err := config.ValidateTokenSwitch(input); err != nil {
		return err
	}
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	previous := state.Config.TokenSwitch
	if _, err := s.runtime.UpdateConfig(func(cfg *config.AppConfig) error {
		cfg.TokenSwitch = input
		return nil
	}); err != nil {
		return err
	}
	s.runtime.ClearHealthForTokenSwitchChanges(previous, input)
	if input.Mode != config.TokenSwitchModeAuto {
		// 自动轮次只在本次运行的自动模式中有效；切回手动后必须丢弃尝试集合和自动通知。
		s.switchMu.Lock()
		s.switchRounds = make(map[string]*tokenSwitchRound)
		s.autoSwitchNotices = make(map[string]*PublicDogeTokenSwitchPrompt)
		s.switchMu.Unlock()
	}
	// 修改模式或阈值后重新评估已有运行时状态，使自动模式无需等待下一次请求才接管。
	s.handleHealthChanged()
	return nil
}

// SetDogeAlertSettings 保存余额和套餐提醒的独立开关与美元阈值。
// 关闭提醒不会删除已同步的余额、套餐或公告数据，只是不再生成对应右下角提醒。
func (s *DesktopService) SetDogeAlertSettings(input config.DogeAlertSettings) error {
	if math.IsNaN(input.BalanceThresholdUSD) || math.IsInf(input.BalanceThresholdUSD, 0) || input.BalanceThresholdUSD <= 0 {
		return errors.New("余额提醒阈值必须是大于 0 的数字")
	}
	if math.IsNaN(input.SubscriptionThresholdUSD) || math.IsInf(input.SubscriptionThresholdUSD, 0) || input.SubscriptionThresholdUSD <= 0 {
		return errors.New("套餐提醒阈值必须是大于 0 的数字")
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.Doge.Notifications.BalanceAlertEnabled = input.BalanceEnabled
		cfg.Doge.Notifications.BalanceAlertThresholdUSD = input.BalanceThresholdUSD
		cfg.Doge.Notifications.SubscriptionAlertEnabled = input.SubscriptionEnabled
		cfg.Doge.Notifications.SubscriptionAlertThresholdUSD = input.SubscriptionThresholdUSD
		reconcileDogeQuotaAlertRecords(&cfg.Doge.Notifications, cfg.Doge.Account, cfg.Doge.Subscriptions, time.Now())
		return nil
	})
}

// OpenDogeTopup 使用系统默认浏览器打开二狗子购买入口。
// 购买地址来自最近一次 `/api/user/topup/info` 同步结果，不接受前端传入的任意 URL。
func (s *DesktopService) OpenDogeTopup() error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	topupLink := strings.TrimSpace(state.Config.Doge.Topup.TopupLink)
	if topupLink == "" {
		return errors.New("二狗子暂未提供购买入口")
	}
	return platform.OpenURL(topupLink)
}

// OpenDogeProfile 使用系统默认浏览器打开二狗子用户中心，令牌生成路径由界面说明固定提示。
func (s *DesktopService) OpenDogeProfile() error {
	return platform.OpenURL(dogeProfileURL)
}

// OpenExternalURL 使用系统默认浏览器打开前端传入的外部 HTTP(S) 地址。
// URL 的协议和主机校验由 platform.OpenURL 统一执行，避免 WebView 在应用内处理外链，
// 也避免把任意协议交给操作系统。
func (s *DesktopService) OpenExternalURL(raw string) error {
	return platform.OpenURL(raw)
}

func (s *DesktopService) TestProfile(id string) (TestResult, error) {
	state := s.runtime.State()
	index := config.FindProfileIndex(state.Config.Profiles, id)
	if index < 0 {
		return TestResult{}, errors.New("代理 API 不存在")
	}
	profile := config.CloneProfile(state.Config.Profiles[index])
	target, _ := url.Parse(profile.BaseURL)
	target.Path = relay.JoinTargetPath(target.Path, "/v1/models")
	transport, err := network.BuildTransport(state.Config.Network, network.DetectSystemProxy(), state.Config.ProxyPort)
	if err != nil {
		return TestResult{}, err
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	request, _ := http.NewRequest(http.MethodGet, target.String(), nil)
	request.Header.Set("Authorization", "Bearer "+profile.APIKey)
	for name, value := range profile.Headers {
		request.Header.Set(name, value)
	}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return TestResult{}, fmt.Errorf("连接失败: %s", relay.SanitizeError(err))
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32*1024))
	return TestResult{
		OK: response.StatusCode >= 200 && response.StatusCode < 300, Reachable: true,
		Status: response.StatusCode, DurationMs: time.Since(started).Milliseconds(), URL: target.String(),
	}, nil
}

func publicProfile(profile config.Profile, activeProfiles map[string]string) PublicProfile {
	u, _ := url.Parse(profile.BaseURL)
	preview := ""
	if u != nil {
		u.Path = relay.JoinTargetPath(u.Path, "/v1/responses")
		preview = u.String()
	}
	return PublicProfile{
		ID: profile.ID, Source: profile.Source, Category: profile.Category, Name: profile.Name, BaseURL: profile.BaseURL,
		APIKey: profile.APIKey, Note: profile.Note, Headers: profile.Headers, Models: publicModels(profile.Models), DefaultModel: profile.DefaultModel,
		Active: activeProfiles[profile.Category] == profile.ID, PreviewURL: preview, RemoteTokenID: profile.RemoteTokenID, SkipAutoSwitch: profile.SkipAutoSwitch,
	}
}

func maskDogeKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "**********" + key[len(key)-4:]
}

// dogeTokenNote 为首次同步的令牌生成可读备注；只使用接口返回的掩码密钥和额度摘要，不写入完整密钥。
func dogeTokenNote(token config.DogeToken) string {
	key := strings.TrimSpace(token.MaskedKey)
	if key == "" {
		key = maskDogeKey(token.Key)
	}
	if key == "" {
		return ""
	}
	quota := "不限额度"
	if !token.UnlimitedQuota {
		quota = "剩余 " + strconv.FormatInt(token.RemainQuota, 10)
	}
	return normalizeDogeAPIKey(key) + " · " + quota
}
