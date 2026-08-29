package desktop

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/tasknotify"
)

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
