package desktop

import (
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
	"codexrelay/internal/tasknotify"
)

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
		proxyURLs[category] = clientconfig.ProxyURL(state.Config, category)
	}
	groupDisplayNames := make(map[string]string, len(state.Config.Doge.GroupDisplayNames))
	for group, name := range state.Config.Doge.GroupDisplayNames {
		groupDisplayNames[group] = name
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
		User: state.Config.Doge.User, Groups: append([]string(nil), state.Config.Doge.Groups...), GroupDisplayNames: groupDisplayNames, Tokens: dogeTokens,
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
		Version: applicationVersion, UpdateSupported: updatesSupported(), NeedsOnboarding: s.onboardingStatus(), DataDirectory: s.runtime.DataDirectory(), ProxyPort: state.Config.ProxyPort, ListenOnAllInterfaces: state.Config.ListenOnAllInterfaces, ClientAccessHost: state.Config.ClientAccessHost,
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
