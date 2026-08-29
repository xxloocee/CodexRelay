package desktop

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"codexrelay/internal/config"
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
