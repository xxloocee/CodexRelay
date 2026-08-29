package desktop

import (
	"fmt"
	"math"
	"strings"
	"time"

	"codexrelay/internal/config"
)

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
