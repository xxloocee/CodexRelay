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
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
)

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

// SetDogeBaseURL 保存二狗子管理 API 的服务地址。仍跟随旧默认地址的二狗子
// Profile 会一起切换；用户在编辑页改过地址的 Profile 不会被覆盖。
func (s *DesktopService) SetDogeBaseURL(raw string) error {
	baseURL, err := config.NormalizeDogeBaseURL(raw)
	if err != nil {
		return err
	}
	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	previousBaseURL := strings.TrimRight(strings.TrimSpace(state.Config.Doge.BaseURL), "/")
	if previousBaseURL == baseURL {
		return nil
	}
	return s.updateConfig(func(cfg *config.AppConfig) error {
		oldBaseURL := strings.TrimRight(strings.TrimSpace(cfg.Doge.BaseURL), "/")
		oldProfileURL := oldBaseURL + "/v1"
		newProfileURL := baseURL + "/v1"
		for index := range cfg.Profiles {
			profile := &cfg.Profiles[index]
			if profile.Source != config.SourceDoge {
				continue
			}
			if strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/") == oldProfileURL {
				profile.BaseURL = newProfileURL
			}
		}
		cfg.Doge.BaseURL = baseURL
		return nil
	})
}

// UnbindDoge 删除绑定凭据、账户快照和全部二狗子 Profile。
// Profile、启用映射、故障顺序及对应运行时提示在返回前一并清理，自定义 API 和公告记录不受影响。
func (s *DesktopService) UnbindDoge() error {
	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
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
	if id <= 0 {
		return errors.New("二狗子令牌 ID 无效")
	}
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	next := config.Clone(state.Config)
	profileID, err := upsertDogeTokenProfile(&next, id, true, "")
	if err != nil {
		return err
	}
	profileIndex := config.FindProfileIndex(next.Profiles, profileID)
	if profileIndex < 0 {
		return errors.New("二狗子代理 API 准备失败")
	}
	category := next.Profiles[profileIndex].Category
	previousID := state.Config.ActiveProfiles[category]
	var configResult clientconfig.ConfigureResult
	entry := state.Config.ClientConfigs[category]
	// 兼容旧绑定：只同步已经由 CodexRelay 接管的客户端；尚未接管的
	// 外部配置仍须等待用户在主界面明确确认。
	if clientconfig.Supports(category) && !entry.SkipConfigReplacement {
		status, inspectErr := clientconfig.Inspect(state.Config, category)
		if inspectErr != nil {
			return fmt.Errorf("检查客户端配置失败: %w", inspectErr)
		}
		if status.Status == "error" {
			return fmt.Errorf("检查客户端配置失败: %s", status.Error)
		}
		if status.Configured {
			configResult, err = clientconfig.ConfigureWithResult(next, category, profileID)
			if err != nil {
				return fmt.Errorf("更新客户端配置失败: %w", err)
			}
		}
	}
	err = s.updateConfig(func(cfg *config.AppConfig) error {
		committedID, commitErr := upsertDogeTokenProfile(cfg, id, true, profileID)
		if commitErr != nil {
			return commitErr
		}
		if committedID != profileID {
			return errors.New("二狗子代理 API 已被并发修改，请重试")
		}
		if cfg.ActiveProfiles == nil {
			cfg.ActiveProfiles = map[string]string{}
		}
		cfg.ActiveProfiles[category] = profileID
		return nil
	})
	if err != nil {
		if configResult.Rollback != nil {
			if rollbackErr := configResult.Rollback(); rollbackErr != nil {
				return fmt.Errorf("启用二狗子令牌失败: %v；外部配置回退失败: %w", err, rollbackErr)
			}
		}
		return err
	}
	if previousID != "" && previousID != profileID {
		s.runtime.ResetProfileHealth(previousID)
	}
	return nil
}

// EditDogeToken 使用本地完整密钥创建或补全本地代理 API，但不改变当前启用映射。
// 远端 group 不参与本地类别判断；令牌必须先完成本地类别选择。
func (s *DesktopService) EditDogeToken(id int64) error {
	_, err := s.prepareDogeTokenProfile(id, false)
	return err
}

func (s *DesktopService) prepareDogeTokenProfile(id int64, requireAvailable bool) (string, error) {
	if id <= 0 {
		return "", errors.New("二狗子令牌 ID 无效")
	}
	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	profileID := ""
	err := s.updateConfig(func(cfg *config.AppConfig) error {
		var updateErr error
		profileID, updateErr = upsertDogeTokenProfile(cfg, id, requireAvailable, "")
		return updateErr
	})
	return profileID, err
}

// upsertDogeTokenProfile mutates only the supplied in-memory config. Callers
// can render external client files from a prospective clone, then commit the
// same Profile ID together with ActiveProfiles in one runtime transaction.
func upsertDogeTokenProfile(cfg *config.AppConfig, id int64, requireAvailable bool, preferredProfileID string) (string, error) {
	if cfg == nil {
		return "", errors.New("程序尚未初始化")
	}
	if strings.TrimSpace(cfg.Doge.AccessToken) == "" {
		return "", errors.New("请先绑定二狗子访问令牌")
	}
	tokenIndex := -1
	var remote config.DogeToken
	for index, token := range cfg.Doge.Tokens {
		if token.ID == id {
			tokenIndex = index
			remote = token
			break
		}
	}
	if remote.ID == 0 {
		return "", errors.New("二狗子令牌不存在，请先刷新目录")
	}
	if remote.Category == "" || !config.IsCategory(remote.Category) {
		return "", errors.New("请先为二狗子令牌选择存放类别")
	}
	if requireAvailable && !dogeTokenAvailable(remote, cfg.Doge.Groups) {
		return "", errors.New("二狗子令牌当前分组不可用，不能启用")
	}
	remote.Key = normalizeDogeAPIKey(remote.Key)
	if !isCompleteDogeAPIKey(remote.Key) {
		return "", errors.New("本地没有完整 API 密钥，请先点击手动同步")
	}
	if remote.Note == "" {
		remote.Note = dogeTokenNote(remote)
	}
	cfg.Doge.Tokens[tokenIndex].Key = remote.Key
	cfg.Doge.Tokens[tokenIndex].Note = remote.Note
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
		profile.ID = strings.TrimSpace(preferredProfileID)
		if profile.ID == "" {
			profile.ID = config.NewProfileID()
		}
		cfg.Profiles = append(cfg.Profiles, profile)
		profileIndex = len(cfg.Profiles) - 1
	}
	cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
	return cfg.Profiles[profileIndex].ID, nil
}
