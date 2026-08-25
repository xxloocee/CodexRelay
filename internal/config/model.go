/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 配置模型、克隆与标识生成
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"codexrelay/internal/network"
)

const DefaultProxyPort = 8765

// DefaultDogeSyncIntervalMinutes 是新配置的二狗子自动同步默认间隔。
const DefaultDogeSyncIntervalMinutes = 3

const (
	// TokenSwitchModePrompt 在达到故障条件后显示手动切换提示。
	TokenSwitchModePrompt = "prompt"
	// TokenSwitchModeAuto 在达到故障条件后自动切换到候选 Profile。
	TokenSwitchModeAuto = "auto"

	DefaultAuthFailureThreshold         = 5
	DefaultUpstreamFailureThreshold     = 5
	DefaultUpstreamFailureWindowMinutes = 3
	DefaultQuotaAlertThresholdUSD       = 1
)

const (
	SourceDoge   = "doge"
	SourceCustom = "custom"

	// RestoreViewCurrent 恢复窗口时保留用户当前的主页来源和类别筛选。
	RestoreViewCurrent = "current"
	// RestoreViewDefault 恢复窗口时重新应用 Preferences 中的默认来源和类别。
	RestoreViewDefault = "default"

	CategoryCodex    = "codex"
	CategoryClaude   = "claude"
	CategoryGemini   = "gemini"
	CategoryGrok     = "grok"
	CategoryOpenCode = "opencode"
	CategoryOpenClaw = "openclaw"
	CategoryHermes   = "hermes"
	CategoryImage    = "image"
	CategoryOther    = "other"
)

var Categories = []string{
	CategoryCodex,
	CategoryClaude,
	CategoryGemini,
	CategoryGrok,
	CategoryOpenCode,
	CategoryOpenClaw,
	CategoryHermes,
	CategoryImage,
	CategoryOther,
}

// ClientConfig 保存外部 AI 客户端的配置目录和主配置文件名。
// 路径由启动时按已知默认目录只读探测，用户也可以在高级设置中覆盖；不保存外部文件正文。
type ClientConfig struct {
	ConfigDir             string `json:"configDir,omitempty"`
	ConfigFile            string `json:"configFile,omitempty"`
	SkipConfigReplacement bool   `json:"skipConfigReplacement,omitempty"`
}

// ModelEntry 保存一个代理 API 可用的上游模型；目录来自用户主动获取或手动维护，不推断模型能力。
type ModelEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	OwnedBy       string `json:"ownedBy,omitempty"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
}

type Profile struct {
	ID             string            `json:"id"`
	Source         string            `json:"source"`
	Category       string            `json:"category"`
	Name           string            `json:"name"`
	BaseURL        string            `json:"baseUrl"`
	APIKey         string            `json:"apiKey"`
	Note           string            `json:"note,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Models         []ModelEntry      `json:"models,omitempty"`
	DefaultModel   string            `json:"defaultModel,omitempty"`
	RemoteTokenID  int64             `json:"remoteTokenId,omitempty"`
	SkipAutoSwitch bool              `json:"skipAutoSwitch,omitempty"`
}

// DogeToken 保存二狗子 New API 管理目录；Key 在绑定或手动同步时从密钥接口补全，自动同步只为新增且缺失的令牌补全。
type DogeToken struct {
	ID                 int64   `json:"id"`
	UserID             int64   `json:"userId,omitempty"`
	Key                string  `json:"key,omitempty"`
	MaskedKey          string  `json:"maskedKey,omitempty"`
	Status             int     `json:"status"`
	Name               string  `json:"name"`
	CreatedTime        int64   `json:"createdTime,omitempty"`
	AccessedTime       int64   `json:"accessedTime,omitempty"`
	ExpiredTime        int64   `json:"expiredTime,omitempty"`
	RemainQuota        int64   `json:"remainQuota,omitempty"`
	UnlimitedQuota     bool    `json:"unlimitedQuota"`
	ModelLimitsEnabled bool    `json:"modelLimitsEnabled"`
	ModelLimits        string  `json:"modelLimits,omitempty"`
	AllowIPs           string  `json:"allowIps,omitempty"`
	UsedQuota          int64   `json:"usedQuota,omitempty"`
	Group              string  `json:"group"`                      // 二狗子令牌接口返回的原始分组键，仅用于权限和同组判断。
	GroupDisplayName   string  `json:"groupDisplayName,omitempty"` // /api/user/self/groups 的 display_name，只用于用户可见文案。
	GroupRatio         float64 `json:"groupRatio,omitempty"`
	CrossGroupRetry    bool    `json:"crossGroupRetry"`
	Category           string  `json:"category,omitempty"`
	Note               string  `json:"note,omitempty"`
}

// DogeAccount 保存二狗子当前用户的余额与身份摘要；quota 为上游原始额度单位。
type DogeAccount struct {
	ID           int64  `json:"id,omitempty"`
	Username     string `json:"username,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	Email        string `json:"email,omitempty"`
	Group        string `json:"group,omitempty"`
	Status       int    `json:"status,omitempty"`
	Quota        int64  `json:"quota,omitempty"`
	UsedQuota    int64  `json:"usedQuota,omitempty"`
	RequestCount int64  `json:"requestCount,omitempty"`
}

// DogeSubscription 保存当前有效套餐的额度、状态和到期时间；金额字段为上游原始额度单位。
type DogeSubscription struct {
	ID          int64  `json:"id"`
	PlanID      int64  `json:"planId"`
	PlanTitle   string `json:"planTitle"`
	Status      string `json:"status"`
	AmountTotal int64  `json:"amountTotal"`
	AmountUsed  int64  `json:"amountUsed"`
	StartTime   int64  `json:"startTime"`
	EndTime     int64  `json:"endTime"`
}

// DogeTopupInfo 保存兑换开关与购买入口；不保存兑换记录中的兑换 key。
type DogeTopupInfo struct {
	EnableRedemption bool   `json:"enableRedemption"`
	TopupLink        string `json:"topupLink,omitempty"`
}

// DogeAnnouncement 保存公告接口返回的公开内容；ID 是公告在上游的稳定身份。
type DogeAnnouncement struct {
	ID          int64  `json:"id"`
	Content     string `json:"content"`
	Extra       string `json:"extra,omitempty"`
	PublishDate string `json:"publishDate"`
	Type        string `json:"type"`
}

// DogeBalanceAlertRecord 保存一个账户 ID 的低余额提醒状态；记录存在表示该低余额状态已经触发过提醒。
// Acknowledged 只表示用户是否在提醒窗口点击过“我知道了”，不会改变低余额状态本身。
type DogeBalanceAlertRecord struct {
	AccountID    int64     `json:"accountId"`
	AmountUSD    float64   `json:"amountUsd"`
	ThresholdUSD float64   `json:"thresholdUsd"`
	NotifiedAt   time.Time `json:"notifiedAt"`
	Acknowledged bool      `json:"acknowledged"`
}

// DogeSubscriptionAlertRecord 保存一个套餐 ID 当前生命周期提醒状态。
// State 取值为 low_balance、expiring_soon 或 expired；已确认的 expired 记录会继续保留用于跨同步去重。
// Acknowledged 只表示当前生命周期阶段是否已在提醒窗口确认，不改变套餐本身的余额和状态。
type DogeSubscriptionAlertRecord struct {
	SubscriptionID int64     `json:"subscriptionId"`
	AmountUSD      float64   `json:"amountUsd"`
	ThresholdUSD   float64   `json:"thresholdUsd"`
	State          string    `json:"state"`
	NotifiedAt     time.Time `json:"notifiedAt"`
	Acknowledged   bool      `json:"acknowledged"`
}

// DogeNotificationState 保存公告缓存、未读状态和一次性提醒确认状态。
type DogeNotificationState struct {
	Initialized                   bool                          `json:"initialized"`
	AnnouncementsEnabled          bool                          `json:"announcementsEnabled"`
	BalanceAlertEnabled           bool                          `json:"balanceAlertEnabled"`
	BalanceAlertThresholdUSD      float64                       `json:"balanceAlertThresholdUsd"`
	SubscriptionAlertEnabled      bool                          `json:"subscriptionAlertEnabled"`
	SubscriptionAlertThresholdUSD float64                       `json:"subscriptionAlertThresholdUsd"`
	BalanceAlertRecords           []DogeBalanceAlertRecord      `json:"balanceAlertRecords"`
	SubscriptionAlertRecords      []DogeSubscriptionAlertRecord `json:"subscriptionAlertRecords"`
	CurrentNotice                 string                        `json:"currentNotice,omitempty"`
	Announcements                 []DogeAnnouncement            `json:"announcements"`
	ReadAnnouncementIDs           []int64                       `json:"readAnnouncementIds"`
	DismissedAlertKeys            []string                      `json:"dismissedAlertKeys"`
	LastAnnouncementSyncAt        time.Time                     `json:"lastAnnouncementSyncAt,omitempty"`
	LastAnnouncementSyncError     string                        `json:"lastAnnouncementSyncError,omitempty"`
}

// TokenSwitchSettings 保存所有来源共用的令牌故障处理策略。
// 候选仍限制在当前请求类别；来源只决定候选 Profile 的连接信息，不参与故障处理分流。
type TokenSwitchSettings struct {
	Mode                         string `json:"mode"`
	Trigger401                   bool   `json:"trigger401"`
	Trigger403                   bool   `json:"trigger403"`
	Trigger5xx                   bool   `json:"trigger5xx"`
	TriggerNetwork               bool   `json:"triggerNetwork"`
	TriggerDirectoryInvalid      bool   `json:"triggerDirectoryInvalid"`
	TriggerDirectoryMissing      bool   `json:"triggerDirectoryMissing"`
	AuthFailureThreshold         int    `json:"authFailureThreshold"`
	UpstreamFailureThreshold     int    `json:"upstreamFailureThreshold"`
	UpstreamFailureWindowMinutes int    `json:"upstreamFailureWindowMinutes"`
	Loop                         bool   `json:"loop"`
}

// DogeAlertSettings 是通用设置页编辑的余额和套餐提醒配置；金额单位为美元。
type DogeAlertSettings struct {
	BalanceEnabled           bool    `json:"balanceEnabled"`
	BalanceThresholdUSD      float64 `json:"balanceThresholdUsd"`
	SubscriptionEnabled      bool    `json:"subscriptionEnabled"`
	SubscriptionThresholdUSD float64 `json:"subscriptionThresholdUsd"`
}

type DogeConnection struct {
	BaseURL             string                `json:"baseUrl"`
	AccessToken         string                `json:"accessToken,omitempty"`
	SyncIntervalMinutes int                   `json:"syncIntervalMinutes"`
	User                map[string]any        `json:"user,omitempty"`
	Account             DogeAccount           `json:"account"`
	Subscriptions       []DogeSubscription    `json:"subscriptions"`
	Topup               DogeTopupInfo         `json:"topup"`
	Notifications       DogeNotificationState `json:"notifications"`
	Groups              []string              `json:"groups"`
	Tokens              []DogeToken           `json:"tokens"`
	TokenOrder          []string              `json:"tokenOrder"`
	LastSyncAt          time.Time             `json:"lastSyncAt,omitempty"`
	LastSyncError       string                `json:"lastSyncError,omitempty"`
}

// Preferences 保存窗口生命周期、主页类别可见性、默认筛选和恢复策略；不会改变代理运行时路由。
type Preferences struct {
	CloseToTray       bool     `json:"closeToTray"`
	LaunchAtStartup   bool     `json:"launchAtStartup"`
	StartHidden       bool     `json:"startHidden"`
	VisibleCategories []string `json:"visibleCategories"`
	DefaultSource     string   `json:"defaultSource"`
	DefaultCategory   string   `json:"defaultCategory"`
	RestoreViewMode   string   `json:"restoreViewMode"`
}

// TaskNotificationEvents 描述允许进入本地耐久队列的通知事件。所有事件只访问用户
// 预先填写的完整 URL，不携带任务、账户或令牌的动态内容。
type TaskNotificationEvents struct {
	TaskCompleted          bool `json:"taskCompleted"`
	TaskAborted            bool `json:"taskAborted"`
	TokenRequestFailed     bool `json:"tokenRequestFailed"`
	TokenAutoSwitched      bool `json:"tokenAutoSwitched"`
	TokenAutoSwitchFailed  bool `json:"tokenAutoSwitchFailed"`
	AccountBalanceLow      bool `json:"accountBalanceLow"`
	SubscriptionBalanceLow bool `json:"subscriptionBalanceLow"`
}

// DefaultTaskNotificationEvents 为首次启用消息通知的六类事件全部开启；用户仍可在设置页
// 逐项关闭，明确保存后的空选择不会在后续读取时被重新勾选。
func DefaultTaskNotificationEvents() TaskNotificationEvents {
	return TaskNotificationEvents{
		TaskCompleted:          true,
		TaskAborted:            true,
		TokenRequestFailed:     true,
		TokenAutoSwitched:      true,
		TokenAutoSwitchFailed:  true,
		AccountBalanceLow:      true,
		SubscriptionBalanceLow: true,
	}
}

// TaskNotification 保存本机事件通知的直接访问 URL、事件范围和投递边界。
// URL 由用户完整填写，可在需要消息的查询参数位置使用 {title}、{content} 占位符。
type TaskNotification struct {
	Enabled               bool                   `json:"enabled"`
	WebhookURL            string                 `json:"webhookUrl,omitempty"`
	Events                TaskNotificationEvents `json:"events"`
	EventsInitialized     bool                   `json:"eventsInitialized"`
	IdleGraceSeconds      int                    `json:"idleGraceSeconds"`
	RequestTimeoutSeconds int                    `json:"requestTimeoutSeconds"`
	MaxAttempts           int                    `json:"maxAttempts"`
}

const (
	// DefaultTaskNotificationIdleGraceSeconds 是 rollout 终态后再次确认文件未变化的默认时间。
	DefaultTaskNotificationIdleGraceSeconds = 5
	// DefaultTaskNotificationRequestTimeoutSeconds 限制单次 Webhook 请求，避免后台投递永久阻塞。
	DefaultTaskNotificationRequestTimeoutSeconds = 10
)

// NormalizeTaskNotification 为缺少事件范围的既有本机 watcher 配置补足原有默认值。
// EventsInitialized 用于区分首次升级的空值与用户明确保存的“全不推送”。
func NormalizeTaskNotification(notification TaskNotification) TaskNotification {
	if !notification.EventsInitialized {
		notification.Events = DefaultTaskNotificationEvents()
		notification.EventsInitialized = true
	}
	return notification
}

type AppConfig struct {
	ProxyPort        int                     `json:"proxyPort"`
	LocalAccessToken string                  `json:"localAccessToken"`
	ActiveProfiles   map[string]string       `json:"activeProfiles"`
	Profiles         []Profile               `json:"profiles"`
	FailoverOrder    map[string][]string     `json:"failoverOrder"`
	ClientConfigs    map[string]ClientConfig `json:"clientConfigs"`
	Network          network.Settings        `json:"network"`
	Preferences      Preferences             `json:"preferences"`
	TaskNotification TaskNotification        `json:"taskNotification"`
	Doge             DogeConnection          `json:"doge"`
	TokenSwitch      TokenSwitchSettings     `json:"tokenSwitch"`
}

// DefaultTokenSwitchSettings 返回兼容当前版本行为的故障处理默认值。
func DefaultTokenSwitchSettings() TokenSwitchSettings {
	return TokenSwitchSettings{
		Mode:                         TokenSwitchModePrompt,
		Trigger401:                   true,
		Trigger403:                   true,
		Trigger5xx:                   true,
		TriggerNetwork:               true,
		TriggerDirectoryInvalid:      true,
		TriggerDirectoryMissing:      true,
		AuthFailureThreshold:         DefaultAuthFailureThreshold,
		UpstreamFailureThreshold:     DefaultUpstreamFailureThreshold,
		UpstreamFailureWindowMinutes: DefaultUpstreamFailureWindowMinutes,
		Loop:                         true,
	}
}

func Default(proxyPort int) AppConfig {
	if proxyPort == 0 {
		proxyPort = DefaultProxyPort
	}
	return AppConfig{
		ProxyPort:        proxyPort,
		LocalAccessToken: newLocalAccessToken(),
		Profiles:         []Profile{},
		FailoverOrder:    map[string][]string{},
		ActiveProfiles:   map[string]string{},
		ClientConfigs:    map[string]ClientConfig{},
		Network:          network.Settings{Mode: "system"},
		Preferences:      Preferences{CloseToTray: true, VisibleCategories: append([]string(nil), Categories...), DefaultSource: "", DefaultCategory: CategoryCodex, RestoreViewMode: RestoreViewCurrent},
		TaskNotification: TaskNotification{Events: DefaultTaskNotificationEvents(), EventsInitialized: true, IdleGraceSeconds: DefaultTaskNotificationIdleGraceSeconds, RequestTimeoutSeconds: DefaultTaskNotificationRequestTimeoutSeconds},
		Doge:             DogeConnection{BaseURL: "https://api.ergouzi.life", SyncIntervalMinutes: DefaultDogeSyncIntervalMinutes, Notifications: DogeNotificationState{BalanceAlertEnabled: true, BalanceAlertThresholdUSD: DefaultQuotaAlertThresholdUSD, SubscriptionAlertEnabled: true, SubscriptionAlertThresholdUSD: DefaultQuotaAlertThresholdUSD, BalanceAlertRecords: []DogeBalanceAlertRecord{}, SubscriptionAlertRecords: []DogeSubscriptionAlertRecord{}, Announcements: []DogeAnnouncement{}, ReadAnnouncementIDs: []int64{}, DismissedAlertKeys: []string{}}, Groups: []string{}, Tokens: []DogeToken{}, TokenOrder: []string{}},
		TokenSwitch:      DefaultTokenSwitchSettings(),
	}
}

func Clone(source AppConfig) AppConfig {
	clone := source
	clone.Preferences.VisibleCategories = append([]string(nil), source.Preferences.VisibleCategories...)
	clone.Profiles = make([]Profile, len(source.Profiles))
	for i := range source.Profiles {
		clone.Profiles[i] = CloneProfile(source.Profiles[i])
	}
	clone.ActiveProfiles = make(map[string]string, len(source.ActiveProfiles))
	for category, id := range source.ActiveProfiles {
		clone.ActiveProfiles[category] = id
	}
	clone.FailoverOrder = make(map[string][]string, len(source.FailoverOrder))
	for category, ids := range source.FailoverOrder {
		clone.FailoverOrder[category] = append([]string(nil), ids...)
	}
	clone.ClientConfigs = make(map[string]ClientConfig, len(source.ClientConfigs))
	for category, client := range source.ClientConfigs {
		clone.ClientConfigs[category] = client
	}
	clone.Doge.Groups = append([]string(nil), source.Doge.Groups...)
	clone.Doge.Tokens = make([]DogeToken, len(source.Doge.Tokens))
	copy(clone.Doge.Tokens, source.Doge.Tokens)
	clone.Doge.TokenOrder = append([]string(nil), source.Doge.TokenOrder...)
	clone.Doge.Subscriptions = append([]DogeSubscription(nil), source.Doge.Subscriptions...)
	clone.Doge.Notifications.BalanceAlertRecords = append([]DogeBalanceAlertRecord(nil), source.Doge.Notifications.BalanceAlertRecords...)
	clone.Doge.Notifications.SubscriptionAlertRecords = append([]DogeSubscriptionAlertRecord(nil), source.Doge.Notifications.SubscriptionAlertRecords...)
	clone.Doge.Notifications.Announcements = append([]DogeAnnouncement(nil), source.Doge.Notifications.Announcements...)
	clone.Doge.Notifications.ReadAnnouncementIDs = append([]int64(nil), source.Doge.Notifications.ReadAnnouncementIDs...)
	clone.Doge.Notifications.DismissedAlertKeys = append([]string(nil), source.Doge.Notifications.DismissedAlertKeys...)
	if source.Doge.User != nil {
		clone.Doge.User = make(map[string]any, len(source.Doge.User))
		for key, value := range source.Doge.User {
			clone.Doge.User[key] = value
		}
	}
	return clone
}

func CloneProfile(source Profile) Profile {
	clone := source
	if source.Headers != nil {
		clone.Headers = make(map[string]string, len(source.Headers))
		for key, value := range source.Headers {
			clone.Headers[key] = value
		}
	}
	clone.Models = append([]ModelEntry(nil), source.Models...)
	return clone
}

func FindProfileIndex(profiles []Profile, id string) int {
	if id == "" {
		return -1
	}
	for i := range profiles {
		if profiles[i].ID == id {
			return i
		}
	}
	return -1
}

func OrderProfiles(profiles []Profile, ids []string) ([]Profile, error) {
	if len(ids) != len(profiles) {
		return nil, errors.New("排序列表与中转站数量不一致")
	}
	byID := make(map[string]Profile, len(profiles))
	for _, profile := range profiles {
		byID[profile.ID] = profile
	}
	ordered := make([]Profile, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, errors.New("排序列表包含重复中转站")
		}
		profile, ok := byID[id]
		if !ok {
			return nil, errors.New("排序列表包含未知中转站")
		}
		seen[id] = true
		ordered = append(ordered, profile)
	}
	return ordered, nil
}

// NormalizeFailoverOrder 按类别清理并补全 Profile 顺序。
// 已保存的顺序优先，新增或旧配置中缺失的 Profile 按 profiles 当前顺序追加；未知引用和重复引用会被丢弃。
func NormalizeFailoverOrder(order map[string][]string, profiles []Profile) map[string][]string {
	result := make(map[string][]string, len(Categories))
	known := make(map[string]map[string]struct{}, len(Categories))
	for _, category := range Categories {
		known[category] = make(map[string]struct{})
	}
	for _, profile := range profiles {
		if !IsCategory(profile.Category) || strings.TrimSpace(profile.ID) == "" {
			continue
		}
		known[profile.Category][profile.ID] = struct{}{}
	}
	for _, category := range Categories {
		seen := make(map[string]struct{})
		for _, id := range order[category] {
			if _, ok := known[category][id]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			result[category] = append(result[category], id)
		}
		for _, profile := range profiles {
			if profile.Category != category {
				continue
			}
			if _, ok := seen[profile.ID]; ok {
				continue
			}
			seen[profile.ID] = struct{}{}
			result[category] = append(result[category], profile.ID)
		}
	}
	return result
}

func NewProfileID() string {
	return randomToken(12)
}

// IsDogeSyncInterval 判断二狗子自动同步间隔是否为界面和服务端共同支持的分钟数。
func IsDogeSyncInterval(minutes int) bool {
	switch minutes {
	case 1, 3, 5, 10, 15, 30, 60:
		return true
	default:
		return false
	}
}

func randomToken(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("relay-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func newLocalAccessToken() string {
	return "sk-" + randomToken(24)
}
