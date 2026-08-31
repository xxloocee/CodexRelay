package desktop

import (
	"codexrelay/internal/config"
	"codexrelay/internal/network"
	"codexrelay/internal/tasknotify"
	"codexrelay/internal/usage"
)

type DesktopState struct {
	Version         string `json:"version"`
	UpdateSupported bool   `json:"updateSupported"`
	NeedsOnboarding bool   `json:"needsOnboarding"`
	DataDirectory   string `json:"dataDirectory"`
	ProxyPort       int    `json:"proxyPort"`
	// ListenOnAllInterfaces 是网络设置页展示的监听范围，不代表当前出站网络出口。
	ListenOnAllInterfaces bool                       `json:"listenOnAllInterfaces"`
	ClientAccessHost      string                     `json:"clientAccessHost"`
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
	GroupDisplayNames             map[string]string                       `json:"groupDisplayNames"`
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

// DogeTokenCreateInput 描述使用二狗子 User 权限令牌创建新 API 密钥所需的最小参数。
// 分组值必须使用 /api/user/self/groups 返回的原始 key，名称由用户自行填写。
type DogeTokenCreateInput struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

type PublicProfile struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Category string `json:"category"`
	Name     string `json:"name"`
	BaseURL  string `json:"baseUrl"`
	// APIKey 始终为空，保留字段是为了兼容已有 Wails/前端模型；完整密钥只在
	// 后端配置中保存，不再通过状态快照下发。编辑已有 Profile 时提交空值表示
	// 保留原密钥，新增 Profile 仍必须提交完整密钥。
	APIKey           string            `json:"apiKey"`
	APIKeyConfigured bool              `json:"apiKeyConfigured"`
	APIKeyHint       string            `json:"apiKeyHint,omitempty"`
	Note             string            `json:"note,omitempty"`
	Headers          map[string]string `json:"headers"`
	Models           []PublicModel     `json:"models"`
	DefaultModel     string            `json:"defaultModel"`
	Active           bool              `json:"active"`
	PreviewURL       string            `json:"previewUrl"`
	RemoteTokenID    int64             `json:"remoteTokenId,omitempty"`
	SkipAutoSwitch   bool              `json:"skipAutoSwitch"`
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
	ID       string `json:"id"`
	Source   string `json:"source"`
	Category string `json:"category"`
	// DogeGroup 仅用于编辑已导入的二狗子令牌。空值表示不修改远端分组；
	// 真实更新必须经由当前绑定的 User 权限访问令牌完成。
	DogeGroup string `json:"dogeGroup,omitempty"`
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	// APIKey 为空时表示编辑场景下保留已有密钥；新增 Profile 必须填写完整密钥。
	APIKey       string            `json:"apiKey,omitempty"`
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
