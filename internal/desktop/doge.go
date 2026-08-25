/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 二狗子 New API 连接、目录同步与令牌导入
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/network"
	"codexrelay/internal/relay"
	"codexrelay/internal/tasknotify"
)

const defaultDogeBaseURL = "https://api.ergouzi.life"

const (
	subscriptionAlertStateLowBalance   = "low_balance"
	subscriptionAlertStateExpiringSoon = "expiring_soon"
	subscriptionAlertStateExpired      = "expired"
	dogeExpiredSubscriptionRetention   = 7 * 24 * time.Hour
)

type dogeEnvelope struct {
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Success bool            `json:"success"`
}

type dogeStatusResponse struct {
	AnnouncementsEnabled bool                      `json:"announcements_enabled"`
	Announcements        []config.DogeAnnouncement `json:"announcements"`
}

type dogeTokenResponse struct {
	ID                 int64  `json:"id"`
	UserID             int64  `json:"user_id"`
	Key                string `json:"key"`
	Status             int    `json:"status"`
	Name               string `json:"name"`
	CreatedTime        int64  `json:"created_time"`
	AccessedTime       int64  `json:"accessed_time"`
	ExpiredTime        int64  `json:"expired_time"`
	RemainQuota        int64  `json:"remain_quota"`
	UnlimitedQuota     bool   `json:"unlimited_quota"`
	ModelLimitsEnabled bool   `json:"model_limits_enabled"`
	ModelLimits        string `json:"model_limits"`
	AllowIPs           string `json:"allow_ips"`
	UsedQuota          int64  `json:"used_quota"`
	Group              string `json:"group"`
	CrossGroupRetry    bool   `json:"cross_group_retry"`
}

type dogeTokenPage struct {
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Total    int                 `json:"total"`
	Items    []dogeTokenResponse `json:"items"`
}

type dogeGroupInfo struct {
	DisplayName string
	Ratio       float64
}

type dogeUserResponse struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	Email        string `json:"email"`
	Group        string `json:"group"`
	Status       int    `json:"status"`
	Quota        int64  `json:"quota"`
	UsedQuota    int64  `json:"used_quota"`
	RequestCount int64  `json:"request_count"`
}

type dogeSubscriptionResponse struct {
	Subscriptions []dogeSubscriptionItem `json:"subscriptions"`
}

type dogeSubscriptionItem struct {
	Subscription struct {
		ID          int64  `json:"id"`
		PlanID      int64  `json:"plan_id"`
		AmountTotal int64  `json:"amount_total"`
		AmountUsed  int64  `json:"amount_used"`
		StartTime   int64  `json:"start_time"`
		EndTime     int64  `json:"end_time"`
		Status      string `json:"status"`
	} `json:"subscription"`
	Plan struct {
		Title string `json:"title"`
	} `json:"plan"`
}

type dogeTopupInfoResponse struct {
	EnableRedemption bool   `json:"enable_redemption"`
	TopupLink        string `json:"topup_link"`
}

type dogeAnnouncementSnapshot struct {
	Status        dogeStatusResponse
	CurrentNotice string
}

const (
	dogeSyncPhaseBase       = "base"
	dogeSyncPhaseKeys       = "keys"
	dogeTokenKeyConcurrency = 8
)

// dogeSyncMode 区分用户主动要求的完整同步和后台定时的元数据同步。
// 完整同步必须刷新每个令牌的密钥；元数据同步只为本地没有完整密钥的令牌补全。
type dogeSyncMode uint8

const (
	dogeSyncMetadata dogeSyncMode = iota
	dogeSyncFull
)

func (s *DesktopService) dogeSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := s.runtime.State()
			if state == nil {
				continue
			}
			interval := time.Duration(state.Config.Doge.SyncIntervalMinutes) * time.Minute
			if interval <= 0 {
				interval = time.Duration(config.DefaultDogeSyncIntervalMinutes) * time.Minute
			}
			announcementDue := state.Config.Doge.Notifications.LastAnnouncementSyncAt.IsZero() || time.Since(state.Config.Doge.Notifications.LastAnnouncementSyncAt) >= interval
			accountBound := strings.TrimSpace(state.Config.Doge.AccessToken) != ""
			accountDue := accountBound && (state.Config.Doge.LastSyncAt.IsZero() || time.Since(state.Config.Doge.LastSyncAt) >= interval)
			// 账户同步的基础阶段已经包含公告；只有账户未绑定或账户数据尚未到期时，才单独同步公告。
			if announcementDue && !accountDue {
				_ = s.SyncDogeAnnouncements()
			}
			if accountDue {
				_ = s.syncDoge(context.Background(), "", false, dogeSyncMetadata)
			}
		}
	}
}

func (s *DesktopService) BindDoge(accessToken string) error {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return errors.New("二狗子访问令牌不能为空")
	}
	state := s.runtime.State()
	firstBinding := state == nil || strings.TrimSpace(state.Config.Doge.AccessToken) == ""
	if firstBinding {
		s.setDogeAlertsSuppressed(true)
		defer s.setDogeAlertsSuppressed(false)
	}
	if err := s.syncDoge(context.Background(), accessToken, true, dogeSyncFull); err != nil {
		return err
	}
	if err := s.runtime.ActivatePortablePersistence(); err != nil {
		return fmt.Errorf("保存首次初始化数据: %w", err)
	}
	s.setNeedsOnboarding(false)
	return nil
}

// SyncDoge 执行用户主动触发的完整同步；已有令牌密钥也必须重新请求，只有后台定时同步使用元数据模式。
func (s *DesktopService) SyncDoge() error {
	state := s.runtime.State()
	if state == nil || strings.TrimSpace(state.Config.Doge.AccessToken) == "" {
		return errors.New("请先绑定二狗子访问令牌")
	}
	return s.syncDoge(context.Background(), "", false, dogeSyncFull)
}

// SyncDogeAnnouncements 获取公开公告和当前通知；该请求不依赖二狗子账户绑定状态。
func (s *DesktopService) SyncDogeAnnouncements() error {
	return s.syncDogeAnnouncements(context.Background())
}

// MarkDogeAnnouncementsRead 将当前缓存中指定的公告标记为已读，并同步关闭对应公告提醒。
func (s *DesktopService) MarkDogeAnnouncementsRead(ids []int64) error {
	return s.updateConfig(func(cfg *config.AppConfig) error {
		known := make(map[int64]struct{}, len(cfg.Doge.Notifications.Announcements))
		for _, announcement := range cfg.Doge.Notifications.Announcements {
			known[announcement.ID] = struct{}{}
		}
		read := append([]int64(nil), cfg.Doge.Notifications.ReadAnnouncementIDs...)
		dismissed := append([]string(nil), cfg.Doge.Notifications.DismissedAlertKeys...)
		for _, id := range ids {
			if _, ok := known[id]; !ok || id <= 0 {
				continue
			}
			read = appendUniqueInt64(read, id)
			dismissed = appendUniqueString(dismissed, announcementAlertKey(id))
		}
		cfg.Doge.Notifications.ReadAnnouncementIDs = read
		cfg.Doge.Notifications.DismissedAlertKeys = dismissed
		return nil
	})
}

// DismissDogeNotification 一次确认当前类别窗口中的全部内容。
// 余额和套餐都只标记当前状态；过期套餐同样保留按套餐 ID 的已确认记录，避免后续同步重复提醒。
// 公告仍使用原有的已读状态，避免公告提醒与额度状态混用。
func (s *DesktopService) DismissDogeNotification(kind string) error {
	return s.updateConfig(func(cfg *config.AppConfig) error {
		notifications := &cfg.Doge.Notifications
		switch kind {
		case NotificationKindBalance:
			if notifications.BalanceAlertEnabled && cfg.Doge.Account.ID > 0 && dogeQuotaToUSD(cfg.Doge.Account.Quota) < notifications.BalanceAlertThresholdUSD {
				for index := range notifications.BalanceAlertRecords {
					if notifications.BalanceAlertRecords[index].AccountID == cfg.Doge.Account.ID {
						notifications.BalanceAlertRecords[index].Acknowledged = true
					}
				}
			}
		case NotificationKindSubscription:
			for index := range notifications.SubscriptionAlertRecords {
				if notifications.SubscriptionAlertEnabled {
					notifications.SubscriptionAlertRecords[index].Acknowledged = true
				}
			}
		case NotificationKindAnnouncement:
			dismissed := append([]string(nil), notifications.DismissedAlertKeys...)
			read := append([]int64(nil), notifications.ReadAnnouncementIDs...)
			for _, announcement := range notifications.Announcements {
				if announcement.ID <= 0 {
					continue
				}
				read = appendUniqueInt64(read, announcement.ID)
				dismissed = appendUniqueString(dismissed, announcementAlertKey(announcement.ID))
			}
			notifications.ReadAnnouncementIDs = read
			notifications.DismissedAlertKeys = dismissed
		default:
			return errors.New("未知的二狗子提醒类别")
		}
		return nil
	})
}

func (s *DesktopService) SetDogeSyncInterval(minutes int) error {
	if !config.IsDogeSyncInterval(minutes) {
		return errors.New("同步间隔必须是 1、3、5、10、15、30 或 60 分钟")
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.Doge.SyncIntervalMinutes = minutes
		return nil
	})
}

// UnbindDoge 删除绑定凭据、账户快照和全部二狗子 Profile。
// Profile、启用映射、故障顺序及对应运行时提示在返回前一并清理，自定义 API 和公告记录不受影响。
func (s *DesktopService) UnbindDoge() error {
	removedProfileIDs := make([]string, 0)
	affectedCategories := make(map[string]struct{})
	_, err := s.runtime.UpdateConfig(func(cfg *config.AppConfig) error {
		for _, profile := range cfg.Profiles {
			if profile.Source != config.SourceDoge {
				continue
			}
			removedProfileIDs = append(removedProfileIDs, profile.ID)
			if cfg.ActiveProfiles[profile.Category] == profile.ID {
				affectedCategories[profile.Category] = struct{}{}
			}
		}
		removeMissingDogeProfiles(cfg, nil)
		cfg.Doge.AccessToken = ""
		cfg.Doge.User = nil
		cfg.Doge.Account = config.DogeAccount{}
		cfg.Doge.Subscriptions = []config.DogeSubscription{}
		cfg.Doge.Topup = config.DogeTopupInfo{}
		cfg.Doge.Groups = []string{}
		cfg.Doge.Tokens = []config.DogeToken{}
		cfg.Doge.TokenOrder = []string{}
		cfg.Doge.LastSyncAt = time.Time{}
		cfg.Doge.LastSyncError = ""
		cfg.Doge.Notifications.BalanceAlertRecords = []config.DogeBalanceAlertRecord{}
		cfg.Doge.Notifications.SubscriptionAlertRecords = []config.DogeSubscriptionAlertRecord{}
		return nil
	})
	if err != nil {
		return err
	}

	// 解绑后的目录和故障状态不能继续引用已删除 Profile，否则独立提醒仍可能展示失效候选。
	removed := make(map[string]struct{}, len(removedProfileIDs))
	for _, profileID := range removedProfileIDs {
		removed[profileID] = struct{}{}
	}
	s.switchMu.Lock()
	s.directorySwitches = make(map[string]*tokenSwitchContext)
	for key := range s.switchPrompts {
		for profileID := range removed {
			if strings.HasPrefix(key, profileID+"|") {
				delete(s.switchPrompts, key)
				break
			}
		}
	}
	for category := range affectedCategories {
		delete(s.switchRounds, category)
		delete(s.autoSwitchNotices, category)
	}
	for category, notice := range s.autoSwitchNotices {
		if notice != nil {
			if _, wasRemoved := removed[notice.CurrentProfileID]; wasRemoved {
				delete(s.autoSwitchNotices, category)
				delete(s.switchRounds, category)
			}
		}
	}
	s.switchMu.Unlock()
	for _, profileID := range removedProfileIDs {
		s.runtime.ResetProfileHealth(profileID)
	}
	s.notifyStateChanged()
	return nil
}

// RedeemDoge 使用当前绑定令牌兑换额度；兑换成功后重新同步用户、套餐和购买配置。
// 兑换码只存在于本次请求体和上游调用栈，不写入配置、日志或返回状态。
func (s *DesktopService) RedeemDoge(code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("兑换码不能为空")
	}
	state := s.runtime.State()
	if state == nil || strings.TrimSpace(state.Config.Doge.AccessToken) == "" {
		return errors.New("请先绑定二狗子访问令牌")
	}
	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	current := s.runtime.State()
	if !current.Config.Doge.Topup.EnableRedemption {
		return errors.New("当前账户暂未开放兑换额度")
	}
	baseURL := strings.TrimSpace(current.Config.Doge.BaseURL)
	if baseURL == "" {
		baseURL = defaultDogeBaseURL
	}
	client, err := s.newDogeHTTPClient()
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	if _, err := s.dogeRequestJSONWithClient(context.Background(), client, baseURL, current.Config.Doge.AccessToken, http.MethodPost, "/api/user/topup", map[string]string{"key": code}, code); err != nil {
		return err
	}
	s.setDogeSyncing(false, true)
	s.setDogeSyncing(true, true)
	s.setDogeSyncPhase(dogeSyncPhaseBase)
	defer func() {
		s.setDogeSyncing(false, false)
		s.setDogeSyncing(true, false)
		s.setDogeSyncPhase("")
	}()
	data, announcements, err := s.fetchDogeData(context.Background(), baseURL, current.Config.Doge.AccessToken, current.Config.Doge.Tokens, dogeSyncFull, client)
	if err != nil {
		_ = s.recordDogeSyncError(err.Error())
		return err
	}
	return s.saveDogeData(data, announcements, baseURL, current.Config.Doge.AccessToken, current.Config.Doge.TokenOrder)
}

func (s *DesktopService) EnableDogeToken(id int64) error {
	return s.prepareDogeTokenProfile(id, true)
}

// EditDogeToken 使用本地完整密钥创建或补全本地代理 API，但不改变当前启用映射。
// 远端 group 不参与本地类别判断；令牌必须先完成本地类别选择。
func (s *DesktopService) EditDogeToken(id int64) error {
	return s.prepareDogeTokenProfile(id, false)
}

func (s *DesktopService) prepareDogeTokenProfile(id int64, activate bool) error {
	if id <= 0 {
		return errors.New("二狗子令牌 ID 无效")
	}
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	if strings.TrimSpace(state.Config.Doge.AccessToken) == "" {
		return errors.New("请先绑定二狗子访问令牌")
	}

	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	current := s.runtime.State()
	var remote config.DogeToken
	for _, token := range current.Config.Doge.Tokens {
		if token.ID == id {
			remote = token
			break
		}
	}
	if remote.ID == 0 {
		return errors.New("二狗子令牌不存在，请先刷新目录")
	}
	if remote.Category == "" || !config.IsCategory(remote.Category) {
		return errors.New("请先为二狗子令牌选择存放类别")
	}
	if activate && !dogeTokenAvailable(remote, current.Config.Doge.Groups) {
		return errors.New("二狗子令牌当前分组不可用，不能启用")
	}
	remote.Key = normalizeDogeAPIKey(remote.Key)
	if !isCompleteDogeAPIKey(remote.Key) {
		return errors.New("本地没有完整 API 密钥，请先点击手动同步")
	}
	if remote.Note == "" {
		remote.Note = dogeTokenNote(remote)
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		index := -1
		for i := range cfg.Doge.Tokens {
			if cfg.Doge.Tokens[i].ID == id {
				cfg.Doge.Tokens[i].Key = remote.Key
				cfg.Doge.Tokens[i].Note = remote.Note
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("二狗子令牌已被刷新移除")
		}
		profileIndex := -1
		for i := range cfg.Profiles {
			if cfg.Profiles[i].Source == config.SourceDoge && cfg.Profiles[i].RemoteTokenID == id {
				profileIndex = i
				break
			}
		}
		profile := config.Profile{
			Source: config.SourceDoge, Category: remote.Category,
			Name: remote.Name, BaseURL: strings.TrimRight(cfg.Doge.BaseURL, "/") + "/v1",
			APIKey: remote.Key, Note: remote.Note, RemoteTokenID: id,
		}
		if profile.Name == "" {
			profile.Name = "二狗子令牌 " + strconv.FormatInt(id, 10)
		}
		if profileIndex >= 0 {
			// 编辑已导入令牌时保留用户修改过的名称、地址、备注、类别和请求头。
			// 远端密钥是唯一由二狗子接口刷新覆盖的字段。
			profile = cfg.Profiles[profileIndex]
			profile.APIKey = remote.Key
			if profile.Note == "" || strings.HasPrefix(profile.Note, "二狗子 · 分组：") {
				profile.Note = remote.Note
			}
			cfg.Profiles[profileIndex] = profile
		} else {
			profile.ID = config.NewProfileID()
			cfg.Profiles = append(cfg.Profiles, profile)
			profileIndex = len(cfg.Profiles) - 1
		}
		cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
		if activate {
			if cfg.ActiveProfiles == nil {
				cfg.ActiveProfiles = map[string]string{}
			}
			cfg.ActiveProfiles[cfg.Profiles[profileIndex].Category] = cfg.Profiles[profileIndex].ID
		}
		return nil
	})
}

func (s *DesktopService) syncDoge(ctx context.Context, accessToken string, replaceToken bool, mode dogeSyncMode) error {
	s.setDogeSyncing(false, true)
	s.setDogeSyncing(true, true)
	s.setDogeSyncPhase(dogeSyncPhaseBase)
	defer s.setDogeSyncing(false, false)
	defer s.setDogeSyncing(true, false)
	defer s.setDogeSyncPhase("")
	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	if !replaceToken {
		accessToken = state.Config.Doge.AccessToken
	}
	baseURL := strings.TrimSpace(state.Config.Doge.BaseURL)
	if baseURL == "" {
		baseURL = defaultDogeBaseURL
	}
	previousTokens := state.Config.Doge.Tokens
	previousOrder := state.Config.Doge.TokenOrder
	if replaceToken {
		previousTokens = nil
		previousOrder = nil
	}
	client, err := s.newDogeHTTPClient()
	if err != nil {
		_ = s.recordDogeSyncError(err.Error())
		return err
	}
	defer client.CloseIdleConnections()
	data, announcements, err := s.fetchDogeData(ctx, baseURL, accessToken, previousTokens, mode, client)
	if err != nil {
		_ = s.recordDogeSyncError(err.Error())
		return err
	}
	directorySwitches := make(map[string]*tokenSwitchContext)
	if !replaceToken {
		directorySwitches = buildDogeDirectorySwitchContexts(state.Config, data)
		// Profile 已在首次缺失同步中删除；后续同步只要远端仍缺失且类别尚未切换，就继续保留同一运行时提示。
		for category, existing := range s.dogeDirectorySwitchContexts() {
			if _, fresh := directorySwitches[category]; fresh || existing == nil || existing.directoryReason != dogeDirectoryFailureMissing {
				continue
			}
			if !directorySwitchContextApplies(state.Config, existing) || dogeTokenDirectoryContains(data.Tokens, existing.profile.RemoteTokenID) {
				continue
			}
			existing.tokens = append([]config.DogeToken(nil), data.Tokens...)
			existing.groups = append([]string(nil), data.Groups...)
			directorySwitches[category] = existing
		}
	}
	if err := s.saveDogeData(data, announcements, baseURL, accessToken, previousOrder); err != nil {
		return err
	}
	s.setDogeDirectorySwitchContexts(directorySwitches)
	return nil
}

func dogeTokenDirectoryContains(tokens []config.DogeToken, remoteTokenID int64) bool {
	for _, token := range tokens {
		if token.ID == remoteTokenID {
			return true
		}
	}
	return false
}

// buildDogeDirectorySwitchContexts 对比同步前后的活动二狗子令牌目录，并按类别保留全部异常。
// 当前令牌消失、状态不再为 1，或所属分组不再可用时生成运行时切换上下文；有效状态 1 来自用户提供的正式接口响应样本。
func buildDogeDirectorySwitchContexts(previous config.AppConfig, current config.DogeConnection) map[string]*tokenSwitchContext {
	previousTokens := make(map[int64]config.DogeToken, len(previous.Doge.Tokens))
	for _, token := range previous.Doge.Tokens {
		previousTokens[token.ID] = token
	}
	currentTokens := make(map[int64]config.DogeToken, len(current.Tokens))
	for _, token := range current.Tokens {
		currentTokens[token.ID] = token
	}
	profilesByRemoteID := make(map[int64]config.Profile)
	for _, profile := range previous.Profiles {
		if profile.Source == config.SourceDoge && profile.RemoteTokenID > 0 {
			profilesByRemoteID[profile.RemoteTokenID] = profile
		}
	}
	result := make(map[string]*tokenSwitchContext)
	for _, category := range config.Categories {
		profileID := previous.ActiveProfiles[category]
		profileIndex := config.FindProfileIndex(previous.Profiles, profileID)
		if profileIndex < 0 {
			continue
		}
		profile := previous.Profiles[profileIndex]
		if profile.Source != config.SourceDoge || profile.RemoteTokenID <= 0 {
			continue
		}
		latest, exists := currentTokens[profile.RemoteTokenID]
		reason := ""
		switch {
		case !exists:
			reason = dogeDirectoryFailureMissing
		case !dogeTokenAvailable(latest, current.Groups):
			reason = dogeDirectoryFailureUnavailable
		default:
			continue
		}
		selected := latest
		if !exists {
			selected = previousTokens[profile.RemoteTokenID]
		}
		if selected.ID == 0 {
			selected = config.DogeToken{
				ID: profile.RemoteTokenID, Name: profile.Name, Category: profile.Category,
			}
		}
		result[category] = &tokenSwitchContext{
			key:         profile.ID + "|directory|" + strconv.FormatInt(profile.RemoteTokenID, 10),
			failureKind: "directory", directoryReason: reason, profile: profile, token: selected,
			profilesByID: profilesByRemoteID, tokens: append([]config.DogeToken(nil), current.Tokens...),
			groups:        append([]string(nil), current.Groups...),
			failoverOrder: append([]string(nil), config.NormalizeFailoverOrder(previous.FailoverOrder, previous.Profiles)[category]...),
		}
	}
	candidateConfig := config.Clone(previous)
	candidateConfig.Doge = current
	for _, context := range result {
		if context != nil {
			context.candidateProfiles = directoryFailoverCandidates(candidateConfig, context)
		}
	}
	return result
}

// buildDogeDirectorySwitchContext 保留单条测试入口，返回类别顺序中的第一条目录异常。
func buildDogeDirectorySwitchContext(previous config.AppConfig, current config.DogeConnection) *tokenSwitchContext {
	contexts := buildDogeDirectorySwitchContexts(previous, current)
	for _, category := range config.Categories {
		if context := contexts[category]; context != nil {
			return context
		}
	}
	return nil
}

func (s *DesktopService) syncDogeAnnouncements(ctx context.Context) error {
	s.setDogeSyncing(true, true)
	defer s.setDogeSyncing(true, false)
	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	baseURL := strings.TrimSpace(state.Config.Doge.BaseURL)
	if baseURL == "" {
		baseURL = defaultDogeBaseURL
	}
	client, err := s.newDogeHTTPClient()
	if err != nil {
		_ = s.recordDogeAnnouncementSyncError(err.Error())
		return err
	}
	defer client.CloseIdleConnections()
	announcements, err := s.fetchDogeAnnouncements(ctx, client, baseURL)
	if err != nil {
		_ = s.recordDogeAnnouncementSyncError(err.Error())
		return err
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.Doge.Notifications = mergeDogeAnnouncementState(cfg.Doge.Notifications, announcements)
		return nil
	})
}

func (s *DesktopService) saveDogeData(data config.DogeConnection, announcements dogeAnnouncementSnapshot, baseURL, accessToken string, previousOrder []string) error {
	data.AccessToken = accessToken
	data.BaseURL = baseURL
	data.TokenOrder = mergeDogeTokenOrder(previousOrder, data.Tokens)
	data.Tokens = orderDogeTokens(data.TokenOrder, data.Tokens)
	removedProfileIDs := make([]string, 0)
	pendingNotificationEvents := make([]taskNotificationEvent, 0, 2)
	err := s.updateConfig(func(cfg *config.AppConfig) error {
		// 同步响应只覆盖远端目录字段；同步间隔由本地设置维护，必须从当前配置继承，避免后台同步把用户选择重置为默认值。
		data.SyncIntervalMinutes = cfg.Doge.SyncIntervalMinutes
		data.Notifications = mergeDogeAnnouncementState(cfg.Doge.Notifications, announcements)
		now := time.Now()
		data.Subscriptions = filterDogeSubscriptionsForStorage(data.Subscriptions, now)
		reconcileDogeQuotaAlertRecords(&data.Notifications, data.Account, data.Subscriptions, now)
		pendingNotificationEvents = collectDogeQuotaNotificationEvents(cfg.Doge.Notifications, data.Notifications)
		data.Notifications.DismissedAlertKeys = pruneDogeAlertKeys(data.Notifications.DismissedAlertKeys, data.Subscriptions, now)
		for i := range data.Tokens {
			if data.Tokens[i].Category != "" {
				continue
			}
			for _, profile := range cfg.Profiles {
				if profile.Source == config.SourceDoge && profile.RemoteTokenID == data.Tokens[i].ID {
					data.Tokens[i].Category = profile.Category
					break
				}
			}
		}
		for profileIndex := range cfg.Profiles {
			profile := &cfg.Profiles[profileIndex]
			if profile.Source != config.SourceDoge || profile.RemoteTokenID <= 0 {
				continue
			}
			for _, token := range data.Tokens {
				if token.ID != profile.RemoteTokenID {
					continue
				}
				if token.Key != "" {
					profile.APIKey = token.Key
				}
				if token.Note == "" {
					break
				}
				if profile.Note == "" || strings.HasPrefix(profile.Note, "二狗子 · 分组：") {
					profile.Note = token.Note
				}
				break
			}
		}
		removedProfileIDs = removeMissingDogeProfiles(cfg, data.Tokens)
		cfg.Doge = data
		return nil
	})
	if err != nil {
		return err
	}
	for _, profileID := range removedProfileIDs {
		s.runtime.ResetProfileHealth(profileID)
	}
	for _, event := range pendingNotificationEvents {
		s.enqueueTaskNotificationEvent(event.Type, event.Identity, event.Details)
	}
	return nil
}

// removeMissingDogeProfiles 以本次成功同步的完整令牌目录清理本地二狗子 Profile。
// 自定义 API 不受影响；被删除项同时退出启用映射和故障顺序，避免旧密钥继续参与路由或切换。
func removeMissingDogeProfiles(cfg *config.AppConfig, tokens []config.DogeToken) []string {
	if cfg == nil {
		return nil
	}
	knownRemoteIDs := make(map[int64]struct{}, len(tokens))
	for _, token := range tokens {
		if token.ID > 0 {
			knownRemoteIDs[token.ID] = struct{}{}
		}
	}
	kept := make([]config.Profile, 0, len(cfg.Profiles))
	removed := make([]string, 0)
	for _, profile := range cfg.Profiles {
		if profile.Source != config.SourceDoge {
			kept = append(kept, profile)
			continue
		}
		if profile.RemoteTokenID > 0 {
			if _, exists := knownRemoteIDs[profile.RemoteTokenID]; exists {
				kept = append(kept, profile)
				continue
			}
		}
		removed = append(removed, profile.ID)
		if cfg.ActiveProfiles[profile.Category] == profile.ID {
			delete(cfg.ActiveProfiles, profile.Category)
		}
	}
	cfg.Profiles = kept
	cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
	return removed
}

func mergeDogeAnnouncementState(notifications config.DogeNotificationState, snapshot dogeAnnouncementSnapshot) config.DogeNotificationState {
	if notifications.Announcements == nil {
		notifications.Announcements = []config.DogeAnnouncement{}
	}
	if notifications.ReadAnnouncementIDs == nil {
		notifications.ReadAnnouncementIDs = []int64{}
	}
	if notifications.DismissedAlertKeys == nil {
		notifications.DismissedAlertKeys = []string{}
	}
	if !notifications.Initialized {
		for _, announcement := range snapshot.Status.Announcements {
			if announcement.ID <= 0 {
				continue
			}
			notifications.ReadAnnouncementIDs = appendUniqueInt64(notifications.ReadAnnouncementIDs, announcement.ID)
			notifications.DismissedAlertKeys = appendUniqueString(notifications.DismissedAlertKeys, announcementAlertKey(announcement.ID))
		}
		notifications.Initialized = true
	}
	notifications.AnnouncementsEnabled = snapshot.Status.AnnouncementsEnabled
	notifications.CurrentNotice = snapshot.CurrentNotice
	notifications.Announcements = snapshot.Status.Announcements
	notifications.ReadAnnouncementIDs = pruneAnnouncementIDs(notifications.ReadAnnouncementIDs, snapshot.Status.Announcements)
	notifications.DismissedAlertKeys = pruneAnnouncementAlertKeys(notifications.DismissedAlertKeys, snapshot.Status.Announcements)
	notifications.LastAnnouncementSyncAt = time.Now()
	notifications.LastAnnouncementSyncError = ""
	return notifications
}

func (s *DesktopService) recordDogeSyncError(message string) error {
	message = strings.TrimSpace(message)
	if len([]rune(message)) > 240 {
		message = string([]rune(message)[:240])
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.Doge.LastSyncError = message
		return nil
	})
}

func (s *DesktopService) recordDogeAnnouncementSyncError(message string) error {
	message = strings.TrimSpace(message)
	if len([]rune(message)) > 240 {
		message = string([]rune(message)[:240])
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		cfg.Doge.Notifications.LastAnnouncementSyncError = message
		return nil
	})
}

func appendUniqueInt64(values []int64, value int64) []int64 {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func pruneAnnouncementIDs(values []int64, announcements []config.DogeAnnouncement) []int64 {
	known := make(map[int64]struct{}, len(announcements))
	for _, announcement := range announcements {
		known[announcement.ID] = struct{}{}
	}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if _, ok := known[value]; ok {
			result = appendUniqueInt64(result, value)
		}
	}
	return result
}

func pruneAnnouncementAlertKeys(values []string, announcements []config.DogeAnnouncement) []string {
	known := make(map[string]struct{}, len(announcements))
	for _, announcement := range announcements {
		if announcement.ID > 0 {
			known[announcementAlertKey(announcement.ID)] = struct{}{}
		}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !strings.HasPrefix(value, "announcement:") {
			result = appendUniqueString(result, value)
			continue
		}
		if _, ok := known[value]; ok {
			result = appendUniqueString(result, value)
		}
	}
	return result
}

// filterDogeSubscriptionsForStorage 只保存上游本次返回的有效套餐，以及到期不超过七天的套餐。
// 上游已删除的套餐不会从旧配置回填；超过保留期的套餐正文和对应提醒记录会在本次同步一起清理。
func filterDogeSubscriptionsForStorage(current []config.DogeSubscription, now time.Time) []config.DogeSubscription {
	result := make([]config.DogeSubscription, 0, len(current))
	for _, subscription := range current {
		if subscription.ID <= 0 {
			continue
		}
		if isDogeSubscriptionActive(subscription, now) {
			result = append(result, subscription)
			continue
		}
		if !isDogeSubscriptionExpiredWithinRetention(subscription, now) {
			continue
		}
		subscription.Status = subscriptionAlertStateExpired
		result = append(result, subscription)
	}
	return result
}

// reconcileDogeQuotaAlertRecords 按账户 ID 和套餐 ID 归并余额提醒状态。
// 钱包使用账户 ID，套餐使用套餐 ID；低余额、24 小时内到期和已过期分别维护生命周期状态。
// 同一状态的后续同步只更新金额，状态切换才重新触发提醒。过期且余额不高于阈值时静默删除。
// 旧版本的 DismissedAlertKeys 会在首次归并时转为已确认记录，之后该列表只保留公告键。
func reconcileDogeQuotaAlertRecords(notifications *config.DogeNotificationState, account config.DogeAccount, subscriptions []config.DogeSubscription, now time.Time) {
	if notifications == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	balanceThreshold := notifications.BalanceAlertThresholdUSD
	if balanceThreshold <= 0 {
		balanceThreshold = config.DefaultQuotaAlertThresholdUSD
	}
	subscriptionThreshold := notifications.SubscriptionAlertThresholdUSD
	if subscriptionThreshold <= 0 {
		subscriptionThreshold = config.DefaultQuotaAlertThresholdUSD
	}

	balanceRecords := append([]config.DogeBalanceAlertRecord(nil), notifications.BalanceAlertRecords...)
	if account.ID > 0 {
		index := findDogeBalanceAlertRecord(balanceRecords, account.ID)
		amount := dogeQuotaToUSD(account.Quota)
		if amount >= balanceThreshold {
			balanceRecords = removeDogeBalanceAlertRecord(balanceRecords, account.ID)
		} else if index < 0 {
			if notifications.BalanceAlertEnabled {
				balanceRecords = append(balanceRecords, config.DogeBalanceAlertRecord{
					AccountID: account.ID, AmountUSD: amount, ThresholdUSD: balanceThreshold,
					NotifiedAt: now, Acknowledged: legacyAlertWasDismissed(notifications.DismissedAlertKeys, balanceAlertKey(account.ID)),
				})
			}
		} else {
			record := &balanceRecords[index]
			thresholdChanged := alertThresholdChanged(record.ThresholdUSD, balanceThreshold)
			record.AmountUSD = amount
			record.ThresholdUSD = balanceThreshold
			if thresholdChanged {
				record.NotifiedAt = now
				record.Acknowledged = false
			}
		}
	}
	notifications.BalanceAlertRecords = balanceRecords

	previousSubscriptions := append([]config.DogeSubscriptionAlertRecord(nil), notifications.SubscriptionAlertRecords...)
	subscriptionRecords := make([]config.DogeSubscriptionAlertRecord, 0, len(previousSubscriptions))
	for _, subscription := range subscriptions {
		if subscription.ID <= 0 || !isDogeSubscriptionTrackable(subscription, now) {
			continue
		}
		amount := dogeQuotaToUSD(subscription.AmountTotal - subscription.AmountUsed)
		index := findDogeSubscriptionAlertRecord(previousSubscriptions, subscription.ID)
		state := dogeSubscriptionAlertState(subscription, amount, subscriptionThreshold, now)
		if state == "" {
			// 已过期且余额不高于阈值，或未达到任何提醒条件；清理旧记录。
			continue
		}
		if index < 0 {
			if notifications.SubscriptionAlertEnabled {
				subscriptionRecords = append(subscriptionRecords, config.DogeSubscriptionAlertRecord{
					SubscriptionID: subscription.ID, AmountUSD: amount, ThresholdUSD: subscriptionThreshold,
					State: state, NotifiedAt: now, Acknowledged: legacyAlertWasDismissed(notifications.DismissedAlertKeys, subscriptionAlertKey(subscription.ID)) ||
						(state == subscriptionAlertStateExpired && legacyAlertWasDismissed(notifications.DismissedAlertKeys, subscriptionExpiredAlertKey(subscription.ID))),
				})
			}
			continue
		}
		record := previousSubscriptions[index]
		previousState := record.State
		if previousState == "" {
			previousState = subscriptionAlertStateLowBalance
		}
		record.AmountUSD = amount
		record.State = state
		if state == subscriptionAlertStateExpired && legacyAlertWasDismissed(notifications.DismissedAlertKeys, subscriptionExpiredAlertKey(subscription.ID)) {
			record.Acknowledged = true
		}
		if previousState != state || alertThresholdChanged(record.ThresholdUSD, subscriptionThreshold) {
			record.ThresholdUSD = subscriptionThreshold
			record.NotifiedAt = now
			record.Acknowledged = false
		}
		subscriptionRecords = append(subscriptionRecords, record)
	}
	notifications.SubscriptionAlertRecords = subscriptionRecords
}

// collectDogeQuotaNotificationEvents 只识别本次同步中新进入的低余额状态。已有提醒
// 继续同步、仅金额变化、套餐临近到期或已过期都不会重复排队，避免按同步周期刷屏。
func collectDogeQuotaNotificationEvents(previous, current config.DogeNotificationState) []taskNotificationEvent {
	events := make([]taskNotificationEvent, 0, 2)
	for _, record := range current.BalanceAlertRecords {
		if record.AccountID <= 0 || findDogeBalanceAlertRecord(previous.BalanceAlertRecords, record.AccountID) >= 0 {
			continue
		}
		events = append(events, taskNotificationEvent{Type: tasknotify.EventAccountBalanceLow, Identity: fmt.Sprintf("%d\x00%s", record.AccountID, record.NotifiedAt.UTC().Format(time.RFC3339Nano)), Details: tasknotify.EventDetails{OccurredAt: record.NotifiedAt, AmountUSD: record.AmountUSD, ThresholdUSD: record.ThresholdUSD}})
	}
	for _, record := range current.SubscriptionAlertRecords {
		previousIndex := findDogeSubscriptionAlertRecord(previous.SubscriptionAlertRecords, record.SubscriptionID)
		previousState := ""
		if previousIndex >= 0 {
			previousState = previous.SubscriptionAlertRecords[previousIndex].State
		}
		if record.SubscriptionID <= 0 || record.State != subscriptionAlertStateLowBalance || previousState == subscriptionAlertStateLowBalance {
			continue
		}
		events = append(events, taskNotificationEvent{Type: tasknotify.EventSubscriptionBalanceLow, Identity: fmt.Sprintf("%d\x00%s", record.SubscriptionID, record.NotifiedAt.UTC().Format(time.RFC3339Nano)), Details: tasknotify.EventDetails{OccurredAt: record.NotifiedAt, AmountUSD: record.AmountUSD, ThresholdUSD: record.ThresholdUSD}})
	}
	return events
}

func alertThresholdChanged(previous, current float64) bool {
	return previous <= 0 || math.Abs(previous-current) > 1e-9
}

func legacyAlertWasDismissed(values []string, key string) bool {
	for _, value := range values {
		if value == key {
			return true
		}
	}
	return false
}

func findDogeBalanceAlertRecord(records []config.DogeBalanceAlertRecord, accountID int64) int {
	for index := range records {
		if records[index].AccountID == accountID {
			return index
		}
	}
	return -1
}

func removeDogeBalanceAlertRecord(records []config.DogeBalanceAlertRecord, accountID int64) []config.DogeBalanceAlertRecord {
	result := records[:0]
	for _, record := range records {
		if record.AccountID != accountID {
			result = append(result, record)
		}
	}
	return result
}

func findDogeSubscriptionAlertRecord(records []config.DogeSubscriptionAlertRecord, subscriptionID int64) int {
	for index := range records {
		if records[index].SubscriptionID == subscriptionID {
			return index
		}
	}
	return -1
}

func isDogeSubscriptionExpired(subscription config.DogeSubscription, now time.Time) bool {
	if subscription.EndTime > 0 && !time.Unix(subscription.EndTime, 0).After(now) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(subscription.Status), subscriptionAlertStateExpired)
}

func isDogeSubscriptionActive(subscription config.DogeSubscription, now time.Time) bool {
	return strings.EqualFold(strings.TrimSpace(subscription.Status), "active") && !isDogeSubscriptionExpired(subscription, now)
}

func isDogeSubscriptionTrackable(subscription config.DogeSubscription, now time.Time) bool {
	return isDogeSubscriptionActive(subscription, now) || isDogeSubscriptionExpiredWithinRetention(subscription, now)
}

func isDogeSubscriptionExpiredWithinRetention(subscription config.DogeSubscription, now time.Time) bool {
	if subscription.EndTime <= 0 {
		return false
	}
	expiresAt := time.Unix(subscription.EndTime, 0)
	return !expiresAt.After(now) && now.Sub(expiresAt) <= dogeExpiredSubscriptionRetention
}

func dogeSubscriptionAlertState(subscription config.DogeSubscription, amount, threshold float64, now time.Time) string {
	if isDogeSubscriptionExpired(subscription, now) {
		if amount > threshold {
			return subscriptionAlertStateExpired
		}
		return ""
	}
	if amount < threshold {
		return subscriptionAlertStateLowBalance
	}
	if amount > threshold && subscription.EndTime > 0 {
		remaining := time.Unix(subscription.EndTime, 0).Sub(now)
		if remaining > 0 && remaining <= 24*time.Hour {
			return subscriptionAlertStateExpiringSoon
		}
	}
	return ""
}

func pruneDogeAlertKeys(values []string, subscriptions []config.DogeSubscription, now time.Time) []string {
	result := make([]string, 0, len(values))
	knownSubscriptions := make(map[int64]config.DogeSubscription, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscription.ID > 0 {
			knownSubscriptions[subscription.ID] = subscription
		}
	}
	for _, value := range values {
		if strings.HasPrefix(value, "announcement:") {
			result = appendUniqueString(result, value)
			continue
		}
		if strings.HasPrefix(value, "subscription-expired:") {
			id, err := strconv.ParseInt(strings.TrimPrefix(value, "subscription-expired:"), 10, 64)
			if err == nil {
				if subscription, ok := knownSubscriptions[id]; ok && isDogeSubscriptionExpired(subscription, now) {
					result = appendUniqueString(result, value)
				}
			}
		}
	}
	return result
}

func (s *DesktopService) fetchDogeData(ctx context.Context, baseURL, accessToken string, previous []config.DogeToken, mode dogeSyncMode, client *http.Client) (config.DogeConnection, dogeAnnouncementSnapshot, error) {
	var (
		user          map[string]any
		account       config.DogeAccount
		groups        []string
		groupInfo     map[string]dogeGroupInfo
		tokens        []config.DogeToken
		subscriptions []config.DogeSubscription
		topup         config.DogeTopupInfo
		announcements dogeAnnouncementSnapshot
	)
	errorsCh := make(chan error, 6)
	var waitGroup sync.WaitGroup
	waitGroup.Add(6)
	go func() {
		defer waitGroup.Done()
		var err error
		user, account, err = s.fetchDogeUser(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子用户信息失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		groups, groupInfo, err = s.fetchDogeGroups(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子分组失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		tokens, err = s.fetchDogeTokens(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子令牌目录失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		subscriptions, err = s.fetchDogeSubscriptions(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子套餐失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		topup, err = s.fetchDogeTopupInfo(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子兑换配置失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		announcements, err = s.fetchDogeAnnouncements(ctx, client, baseURL)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子公告失败: %w", err)
		}
	}()
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		return config.DogeConnection{}, dogeAnnouncementSnapshot{}, err
	}

	// 基础数据全部完成后，才进入令牌密钥阶段；元数据同步若没有新增密钥，会直接跳过该阶段。
	s.setDogeSyncing(true, false)
	if count := countDogeTokenKeys(tokens, previous, mode); count > 0 {
		s.setDogeSyncPhase(dogeSyncPhaseKeys)
	}
	if err := s.syncDogeTokenKeys(ctx, client, baseURL, accessToken, tokens, previous, mode); err != nil {
		return config.DogeConnection{}, dogeAnnouncementSnapshot{}, err
	}
	previousByID := make(map[int64]config.DogeToken, len(previous))
	for _, token := range previous {
		if token.ID > 0 {
			token.Key = normalizeDogeAPIKey(token.Key)
			previousByID[token.ID] = token
		}
	}
	for i := range tokens {
		if previousToken, ok := previousByID[tokens[i].ID]; ok {
			tokens[i].Category = previousToken.Category
			tokens[i].Note = previousToken.Note
			tokens[i].GroupDisplayName = previousToken.GroupDisplayName
			tokens[i].GroupRatio = previousToken.GroupRatio
		}
		if info, ok := groupInfo[tokens[i].Group]; ok {
			tokens[i].GroupDisplayName = info.DisplayName
			tokens[i].GroupRatio = info.Ratio
		} else if len(groupInfo) > 0 {
			// 官方对象响应是当前完整分组目录；找不到的组不能继续沿用旧展示名或倍率。
			tokens[i].GroupDisplayName = ""
			tokens[i].GroupRatio = 0
		}
		if tokens[i].Note == "" {
			tokens[i].Note = dogeTokenNote(tokens[i])
		}
	}
	return config.DogeConnection{BaseURL: baseURL, User: user, Account: account, Subscriptions: subscriptions, Topup: topup, Groups: groups, Tokens: tokens, LastSyncAt: time.Now()}, announcements, nil
}

// syncDogeTokenKeys 将令牌列表中的掩码与本地完整密钥分开处理。
// 远端令牌 ID 是唯一稳定身份：完整同步刷新所有令牌，元数据同步只请求本地缺少密钥的令牌。
func (s *DesktopService) syncDogeTokenKeys(ctx context.Context, client *http.Client, baseURL, accessToken string, tokens, previous []config.DogeToken, mode dogeSyncMode) error {
	previousKeys := make(map[int64]string, len(previous))
	for _, token := range previous {
		if token.ID > 0 {
			previousKeys[token.ID] = normalizeDogeAPIKey(token.Key)
		}
	}
	pending := make([]int, 0, len(tokens))
	for index := range tokens {
		key := previousKeys[tokens[index].ID]
		if mode == dogeSyncMetadata && isCompleteDogeAPIKey(key) {
			tokens[index].Key = key
			continue
		}
		pending = append(pending, index)
	}
	if len(pending) == 0 {
		return nil
	}
	// 每批请求必须全部结束后才能启动下一批，避免滑动窗口持续补发请求。
	for start := 0; start < len(pending); start += dogeTokenKeyConcurrency {
		end := minInt(start+dogeTokenKeyConcurrency, len(pending))
		batchCtx, cancel := context.WithCancel(ctx)
		var batchWaitGroup sync.WaitGroup
		var firstErr error
		var firstErrOnce sync.Once
		for _, index := range pending[start:end] {
			batchWaitGroup.Add(1)
			go func(index int) {
				defer batchWaitGroup.Done()
				key, err := s.fetchDogeTokenKey(batchCtx, client, baseURL, accessToken, tokens[index].ID)
				if err != nil {
					firstErrOnce.Do(func() {
						firstErr = fmt.Errorf("同步二狗子令牌 %d 密钥失败: %w", tokens[index].ID, err)
						cancel()
					})
					return
				}
				tokens[index].Key = normalizeDogeAPIKey(key)
				if !isCompleteDogeAPIKey(tokens[index].Key) {
					firstErrOnce.Do(func() {
						firstErr = fmt.Errorf("二狗子令牌 %d 没有返回完整 API 密钥", tokens[index].ID)
						cancel()
					})
				}
			}(index)
		}
		batchWaitGroup.Wait()
		cancel()
		if firstErr != nil {
			return firstErr
		}
	}
	return nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func countDogeTokenKeys(tokens, previous []config.DogeToken, mode dogeSyncMode) int {
	previousKeys := make(map[int64]string, len(previous))
	for _, token := range previous {
		if token.ID > 0 {
			previousKeys[token.ID] = normalizeDogeAPIKey(token.Key)
		}
	}
	count := 0
	for _, token := range tokens {
		if mode == dogeSyncFull || !isCompleteDogeAPIKey(previousKeys[token.ID]) {
			count++
		}
	}
	return count
}

// fetchDogeAnnouncements 并行获取公告列表和当前公告；结果由账户同步与独立公告同步共同复用。
func (s *DesktopService) fetchDogeAnnouncements(ctx context.Context, client *http.Client, baseURL string) (dogeAnnouncementSnapshot, error) {
	var statusData, noticeData []byte
	errorsCh := make(chan error, 2)
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		var err error
		statusData, err = s.dogeRequestWithClient(ctx, client, baseURL, "", http.MethodGet, "/api/status")
		if err != nil {
			errorsCh <- fmt.Errorf("二狗子公告状态请求失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		noticeData, err = s.dogeRequestWithClient(ctx, client, baseURL, "", http.MethodGet, "/api/notice")
		if err != nil {
			errorsCh <- fmt.Errorf("二狗子当前公告请求失败: %w", err)
		}
	}()
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		return dogeAnnouncementSnapshot{}, err
	}
	var status dogeStatusResponse
	if err := json.Unmarshal(statusData, &status); err != nil {
		return dogeAnnouncementSnapshot{}, fmt.Errorf("二狗子公告状态格式无效: %w", err)
	}
	var currentNotice string
	if len(noticeData) > 0 && string(noticeData) != "null" {
		if err := json.Unmarshal(noticeData, &currentNotice); err != nil {
			return dogeAnnouncementSnapshot{}, fmt.Errorf("二狗子当前公告格式无效: %w", err)
		}
	}
	return dogeAnnouncementSnapshot{Status: status, CurrentNotice: currentNotice}, nil
}

func (s *DesktopService) fetchDogeUser(ctx context.Context, client *http.Client, baseURL, accessToken string) (map[string]any, config.DogeAccount, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, "/api/user/self")
	if err != nil {
		return nil, config.DogeAccount{}, err
	}
	var user map[string]any
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, config.DogeAccount{}, fmt.Errorf("二狗子用户信息格式无效: %w", err)
	}
	var typed dogeUserResponse
	if err := json.Unmarshal(data, &typed); err != nil {
		return nil, config.DogeAccount{}, fmt.Errorf("二狗子用户额度格式无效: %w", err)
	}
	return sanitizeDogeUser(user), config.DogeAccount{ID: typed.ID, Username: typed.Username, DisplayName: typed.DisplayName, Email: typed.Email, Group: typed.Group, Status: typed.Status, Quota: typed.Quota, UsedQuota: typed.UsedQuota, RequestCount: typed.RequestCount}, nil
}

func (s *DesktopService) fetchDogeSubscriptions(ctx context.Context, client *http.Client, baseURL, accessToken string) ([]config.DogeSubscription, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, "/api/subscription/self")
	if err != nil {
		return nil, err
	}
	var payload dogeSubscriptionResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("二狗子套餐信息格式无效: %w", err)
	}
	result := make([]config.DogeSubscription, 0, len(payload.Subscriptions))
	for _, item := range payload.Subscriptions {
		result = append(result, config.DogeSubscription{ID: item.Subscription.ID, PlanID: item.Subscription.PlanID, PlanTitle: item.Plan.Title, Status: item.Subscription.Status, AmountTotal: item.Subscription.AmountTotal, AmountUsed: item.Subscription.AmountUsed, StartTime: item.Subscription.StartTime, EndTime: item.Subscription.EndTime})
	}
	return result, nil
}

func (s *DesktopService) fetchDogeTopupInfo(ctx context.Context, client *http.Client, baseURL, accessToken string) (config.DogeTopupInfo, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, "/api/user/topup/info")
	if err != nil {
		return config.DogeTopupInfo{}, err
	}
	var payload dogeTopupInfoResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return config.DogeTopupInfo{}, fmt.Errorf("二狗子充值配置格式无效: %w", err)
	}
	return config.DogeTopupInfo{EnableRedemption: payload.EnableRedemption, TopupLink: strings.TrimSpace(payload.TopupLink)}, nil
}

func sanitizeDogeUser(user map[string]any) map[string]any {
	result := make(map[string]any)
	for _, key := range []string{"id", "user_id", "username", "display_name", "displayName", "email", "role", "group", "status"} {
		if value, ok := user[key]; ok {
			result[key] = value
		}
	}
	return result
}

func (s *DesktopService) fetchDogeGroups(ctx context.Context, client *http.Client, baseURL, accessToken string) ([]string, map[string]dogeGroupInfo, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, "/api/user/self/groups")
	if err != nil {
		return nil, nil, err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("二狗子分组信息格式无效: %w", err)
	}
	groups, details := parseDogeGroups(raw)
	return groups, details, nil
}

// parseDogeGroups 解析分组键与展示元数据；分组键用于匹配令牌，展示名和倍率只用于界面。
func parseDogeGroups(value any) ([]string, map[string]dogeGroupInfo) {
	if item, ok := value.(map[string]any); ok {
		groups := make([]string, 0, len(item))
		details := make(map[string]dogeGroupInfo, len(item))
		for key, child := range item {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			info := dogeGroupInfo{}
			if metadata, ok := child.(map[string]any); ok {
				if displayName, ok := metadata["display_name"].(string); ok && strings.TrimSpace(displayName) != "" {
					info.DisplayName = strings.TrimSpace(displayName)
				}
				if ratio, ok := metadata["ratio"].(float64); ok {
					info.Ratio = ratio
				}
			}
			// 权限目录保存接口对象键；该键不参与用户界面文案。
			groups = append(groups, key)
			details[key] = info
		}
		if len(details) > 0 {
			return uniqueStrings(groups), details
		}
	}
	return uniqueStrings(collectDogeGroups(value)), map[string]dogeGroupInfo{}
}

func collectDogeGroups(value any) []string {
	switch item := value.(type) {
	case string:
		if strings.TrimSpace(item) != "" {
			return []string{strings.TrimSpace(item)}
		}
	case []any:
		var result []string
		for _, child := range item {
			result = append(result, collectDogeGroups(child)...)
		}
		return result
	case map[string]any:
		var result []string
		known := map[string]bool{"data": true, "groups": true, "items": true, "list": true, "name": true}
		for _, key := range []string{"data", "groups", "items", "list"} {
			if child, ok := item[key]; ok {
				result = append(result, collectDogeGroups(child)...)
			}
		}
		if name, ok := item["name"].(string); ok && len(result) == 0 {
			result = append(result, strings.TrimSpace(name))
		}
		if len(result) == 0 {
			for key := range item {
				if !known[key] && strings.TrimSpace(key) != "" {
					result = append(result, key)
				}
			}
		}
		return result
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// dogeTokenPermitted 按最近一次同步的分组目录判断令牌所属分组是否有权限；目录可能保存展示名或原始分组键。
func dogeTokenPermitted(token config.DogeToken, groups []string) bool {
	group := strings.TrimSpace(token.Group)
	displayName := strings.TrimSpace(token.GroupDisplayName)
	if group == "" && displayName == "" {
		return false
	}
	for _, available := range groups {
		available = strings.TrimSpace(available)
		if available != "" && (available == group || available == displayName) {
			return true
		}
	}
	return false
}

// dogeTokenDisplayGroup 返回令牌在用户界面中的分组文案。
// 分组目录中的原始键只用于权限判断；用户可见名称必须来自同步得到的 display_name，缺失时保持为空。
func dogeTokenDisplayGroup(token config.DogeToken) string {
	return strings.TrimSpace(token.GroupDisplayName)
}

// dogeTokenAvailable 在目录状态和用户分组两个维度判断令牌是否可选择。
// 当前上游样本中 status=1 表示正常令牌；其他状态不进入启用或切换候选。
func dogeTokenAvailable(token config.DogeToken, groups []string) bool {
	return token.Status == 1 && dogeTokenPermitted(token, groups)
}

// dogeTokenSwitchable 判断令牌是否满足主窗口、托盘和切换服务共同使用的可切换约束。
// 完整本地密钥只从配置读取，掩码密钥不能进入启用或切换入口。
func dogeTokenSwitchable(token config.DogeToken, groups []string) bool {
	return dogeTokenAvailable(token, groups) && isCompleteDogeAPIKey(token.Key)
}

// availableDogeTokensForCategory 返回主窗口该类别下可实际切换的二狗子令牌。
// 类别为空时沿用已导入 Profile 的类别；状态、权限和完整本地密钥沿用主窗口可用令牌的后端约束。
// 该集合由托盘菜单、切换提示和切换服务端复核共同使用，不按远端分组额外缩小范围。
func availableDogeTokensForCategory(tokens []config.DogeToken, profilesByRemoteID map[int64]config.Profile, groups []string, category string) []config.DogeToken {
	category = strings.TrimSpace(category)
	if category == "" {
		return nil
	}
	available := make([]config.DogeToken, 0, len(tokens))
	for _, token := range tokens {
		if token.ID <= 0 {
			continue
		}
		resolvedCategory := strings.TrimSpace(token.Category)
		if resolvedCategory == "" {
			if profile, ok := profilesByRemoteID[token.ID]; ok {
				resolvedCategory = strings.TrimSpace(profile.Category)
			}
		}
		if resolvedCategory != category || !dogeTokenSwitchable(token, groups) {
			continue
		}
		token.Category = resolvedCategory
		available = append(available, token)
	}
	return available
}

func (s *DesktopService) fetchDogeTokens(ctx context.Context, client *http.Client, baseURL, accessToken string) ([]config.DogeToken, error) {
	const pageSize = 100
	all := make([]config.DogeToken, 0)
	for page := 1; page <= 100; page++ {
		data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, fmt.Sprintf("/api/token/?p=%d&page_size=%d", page, pageSize))
		if err != nil {
			return nil, err
		}
		var payload dogeTokenPage
		if err := json.Unmarshal(data, &payload); err != nil {
			var direct []dogeTokenResponse
			if directErr := json.Unmarshal(data, &direct); directErr != nil {
				return nil, fmt.Errorf("二狗子令牌列表格式无效: %w", err)
			}
			payload.Items = direct
		}
		for _, item := range payload.Items {
			all = append(all, config.DogeToken{
				ID: item.ID, UserID: item.UserID, MaskedKey: item.Key,
				Status: item.Status, Name: item.Name, CreatedTime: item.CreatedTime,
				AccessedTime: item.AccessedTime, ExpiredTime: item.ExpiredTime,
				RemainQuota: item.RemainQuota, UnlimitedQuota: item.UnlimitedQuota,
				ModelLimitsEnabled: item.ModelLimitsEnabled, ModelLimits: item.ModelLimits,
				AllowIPs: item.AllowIPs, UsedQuota: item.UsedQuota, Group: item.Group,
				CrossGroupRetry: item.CrossGroupRetry,
			})
		}
		if len(payload.Items) == 0 || (payload.Total > 0 && len(all) >= payload.Total) || len(payload.Items) < pageSize {
			break
		}
	}
	return all, nil
}

func (s *DesktopService) fetchDogeTokenKey(ctx context.Context, client *http.Client, baseURL, accessToken string, id int64) (string, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodPost, fmt.Sprintf("/api/token/%d/key", id))
	if err != nil {
		return "", err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("二狗子令牌密钥格式无效: %w", err)
	}
	key := findDogeKey(raw)
	if key == "" {
		return "", errors.New("二狗子接口没有返回完整 API 密钥")
	}
	return normalizeDogeAPIKey(key), nil
}

// normalizeDogeAPIKey 统一二狗子接口返回的令牌格式，避免同一个令牌出现两种前缀。
func normalizeDogeAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "sk-") {
		return key
	}
	return "sk-" + key
}

// isCompleteDogeAPIKey 识别可用于代理认证的本地密钥；二狗子令牌列表的掩码值包含星号，不能直接复用。
func isCompleteDogeAPIKey(key string) bool {
	key = strings.TrimSpace(key)
	return key != "" && !strings.Contains(key, "*")
}

func findDogeKey(value any) string {
	switch item := value.(type) {
	case string:
		return strings.TrimSpace(item)
	case map[string]any:
		if key, ok := item["key"]; ok {
			if value := findDogeKey(key); value != "" {
				return value
			}
		}
		if data, ok := item["data"]; ok {
			return findDogeKey(data)
		}
	}
	return ""
}

func (s *DesktopService) dogeRequest(ctx context.Context, baseURL, accessToken, method, path string) ([]byte, error) {
	return s.dogeRequestBody(ctx, baseURL, accessToken, method, path, nil, "")
}

func (s *DesktopService) dogeRequestJSON(ctx context.Context, baseURL, accessToken, method, path string, payload any, sensitive string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("创建二狗子请求体失败: %w", err)
	}
	return s.dogeRequestBody(ctx, baseURL, accessToken, method, path, bytes.NewReader(body), sensitive)
}

func (s *DesktopService) dogeRequestBody(ctx context.Context, baseURL, accessToken, method, path string, body io.Reader, sensitive string) ([]byte, error) {
	client, err := s.newDogeHTTPClient()
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()
	return s.dogeRequestBodyWithClient(ctx, client, baseURL, accessToken, method, path, body, sensitive)
}

func (s *DesktopService) dogeRequestWithClient(ctx context.Context, client *http.Client, baseURL, accessToken, method, path string) ([]byte, error) {
	return s.dogeRequestBodyWithClient(ctx, client, baseURL, accessToken, method, path, nil, "")
}

func (s *DesktopService) dogeRequestJSONWithClient(ctx context.Context, client *http.Client, baseURL, accessToken, method, path string, payload any, sensitive string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("创建二狗子请求体失败: %w", err)
	}
	return s.dogeRequestBodyWithClient(ctx, client, baseURL, accessToken, method, path, bytes.NewReader(body), sensitive)
}

func (s *DesktopService) newDogeHTTPClient() (*http.Client, error) {
	state := s.runtime.State()
	if state == nil {
		return nil, errors.New("程序尚未初始化")
	}
	transport, err := network.BuildTransport(state.Config.Network, network.DetectSystemProxy(), state.Config.ProxyPort)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}, nil
}

func (s *DesktopService) dogeRequestBodyWithClient(ctx context.Context, client *http.Client, baseURL, accessToken, method, path string, body io.Reader, sensitive string) ([]byte, error) {
	target, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || target.Host == "" {
		return nil, errors.New("二狗子 API 地址无效")
	}
	requestPath, query, hasQuery := strings.Cut(path, "?")
	target.Path = strings.TrimRight(target.Path, "/") + requestPath
	if hasQuery {
		target.RawQuery = query
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("创建二狗子请求失败: %w", err)
	}
	if strings.TrimSpace(accessToken) != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("二狗子请求失败: %s", relay.SanitizeError(err))
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取二狗子响应失败: %w", err)
	}
	var envelope dogeEnvelope
	if decodeErr := json.Unmarshal(responseBody, &envelope); decodeErr != nil {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("二狗子请求返回 HTTP %d", response.StatusCode)
		}
		return nil, fmt.Errorf("二狗子响应格式无效: %w", decodeErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || (!envelope.Success && envelope.Message != "") {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = "请求被拒绝"
		}
		if accessToken != "" {
			message = strings.ReplaceAll(message, accessToken, "[已隐藏]")
		}
		if sensitive != "" {
			message = strings.ReplaceAll(message, sensitive, "[已隐藏]")
		}
		return nil, fmt.Errorf("二狗子请求失败（HTTP %d）: %s", response.StatusCode, message)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return []byte("null"), nil
	}
	return envelope.Data, nil
}

func dogeTokenOrderKey(token config.DogeToken) string {
	if token.ID > 0 {
		return strconv.FormatInt(token.ID, 10)
	}
	return ""
}

func mergeDogeTokenOrder(previous []string, tokens []config.DogeToken) []string {
	known := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if key := dogeTokenOrderKey(token); key != "" {
			known[key] = struct{}{}
		}
	}
	result := make([]string, 0, len(known))
	seen := make(map[string]struct{}, len(known))
	for _, key := range previous {
		key = strings.TrimSpace(key)
		if _, ok := known[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	for _, token := range tokens {
		key := dogeTokenOrderKey(token)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func orderDogeTokens(order []string, tokens []config.DogeToken) []config.DogeToken {
	byKey := make(map[string]config.DogeToken, len(tokens))
	for _, token := range tokens {
		if key := dogeTokenOrderKey(token); key != "" {
			byKey[key] = token
		}
	}
	ordered := make([]config.DogeToken, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, key := range order {
		if token, ok := byKey[key]; ok {
			ordered = append(ordered, token)
			seen[key] = struct{}{}
		}
	}
	for _, token := range tokens {
		key := dogeTokenOrderKey(token)
		if key == "" {
			ordered = append(ordered, token)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ordered = append(ordered, token)
	}
	return ordered
}
