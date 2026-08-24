/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 二狗子接口分页请求回归测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codexrelay/internal/config"
)

func TestBindDogeSendsTokenPaginationQuery(t *testing.T) {
	const accessToken = "fake-doge-access-token"
	var tokenPath string
	var tokenQuery url.Values
	var keyCalls int
	var topupMethod string
	var topupBody []byte
	subscriptionEnd := time.Now().Add(72 * time.Hour).Unix()
	tokenItems := []map[string]any{{
		"id":              42,
		"user_id":         7,
		"key":             "fake********key",
		"status":          1,
		"name":            "测试令牌",
		"group":           "余额低价组",
		"unlimited_quota": true,
	}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicAnnouncement := r.URL.Path == "/api/status" || r.URL.Path == "/api/notice"
		if !publicAnnouncement {
			if got := r.Header.Get("Authorization"); got != "Bearer "+accessToken {
				t.Errorf("authorization header = %q", got)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/status":
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"announcements_enabled": true, "announcements": []any{}}, "message": "", "success": true})
		case r.URL.Path == "/api/notice":
			writeDogeTestJSON(t, w, map[string]any{"data": "", "message": "", "success": true})
		case r.URL.Path == "/api/user/self":
			writeDogeTestJSON(t, w, map[string]any{
				"data":    map[string]any{"id": 7, "username": "fake-user", "display_name": "测试用户", "quota": 1500000, "used_quota": 250000, "request_count": 12},
				"message": "",
				"success": true,
			})
		case r.URL.Path == "/api/user/self/groups":
			writeDogeTestJSON(t, w, map[string]any{
				"data": map[string]any{
					"余额低价组": map[string]any{"display_name": "GPT低价组", "ratio": 0.02},
				},
				"message": "",
				"success": true,
			})
		case r.URL.Path == "/api/subscription/self":
			writeDogeTestJSON(t, w, map[string]any{
				"data": map[string]any{
					"subscriptions": []any{map[string]any{
						"subscription": map[string]any{"id": 9, "plan_id": 13, "amount_total": 2500000, "amount_used": 500000, "start_time": time.Now().Add(-time.Hour).Unix(), "end_time": subscriptionEnd, "status": "active"},
						"plan":         map[string]any{"title": "测试套餐"},
					}},
				},
				"message": "",
				"success": true,
			})
		case r.URL.Path == "/api/user/topup/info":
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"enable_redemption": true, "topup_link": "https://example.test/buy"}, "message": "", "success": true})
		case r.URL.Path == "/api/user/topup":
			topupMethod = r.Method
			topupBody, _ = io.ReadAll(r.Body)
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{}, "message": "", "success": true})
		case strings.HasPrefix(r.URL.Path, "/api/token/") && strings.HasSuffix(r.URL.Path, "/key") && r.Method == http.MethodPost:
			keyCalls++
			key := "full-key-42"
			if strings.Contains(r.URL.Path, "/43/") {
				key = "full-key-43"
			}
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"key": key}, "message": "", "success": true})
		case r.URL.Path == "/api/token/" && r.Method == http.MethodGet:
			tokenPath = r.URL.Path
			tokenQuery = r.URL.Query()
			writeDogeTestJSON(t, w, map[string]any{
				"data": map[string]any{
					"page":      1,
					"page_size": 100,
					"total":     len(tokenItems),
					"items":     tokenItems,
				},
				"message": "",
				"success": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Network.Mode = "direct"
	cfg.Doge.BaseURL = server.URL
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)

	if err := service.BindDoge(accessToken); err != nil {
		t.Fatalf("BindDoge() error = %v", err)
	}
	if tokenPath != "/api/token/" {
		t.Fatalf("token request path = %q", tokenPath)
	}
	if got := tokenQuery.Get("p"); got != "1" {
		t.Fatalf("token page query = %q", got)
	}
	if got := tokenQuery.Get("page_size"); got != "100" {
		t.Fatalf("token page_size query = %q", got)
	}
	if tokens := runtime.State().Config.Doge.Tokens; len(tokens) != 1 || tokens[0].ID != 42 {
		t.Fatalf("synced tokens = %+v", tokens)
	}
	if tokens := runtime.State().Config.Doge.Tokens; len(tokens) != 1 || tokens[0].Key != "sk-full-key-42" || keyCalls != 1 {
		t.Fatalf("manual bind should cache complete key, tokens = %+v, key calls = %d", tokens, keyCalls)
	}
	if tokens := service.GetState().Doge.Tokens; len(tokens) != 1 || !tokens[0].NeedsCategory || !tokens[0].Permitted || tokens[0].MaskedKey != "sk-fake********key" || tokens[0].Note != "sk-fake********key · 不限额度" || tokens[0].GroupDisplayName != "GPT低价组" || tokens[0].GroupRatio != 0.02 {
		t.Fatalf("new token should require category selection: %+v", tokens)
	}
	if state := service.GetState().Doge; state.WalletUSD != 3 || state.SubscriptionsUSD != 4 || state.TotalUSD != 7 || len(state.Subscriptions) != 1 || state.Subscriptions[0].PlanTitle != "测试套餐" {
		t.Fatalf("quota snapshot = %+v", state)
	}
	if account := service.GetState().Doge.Account; account.UserID != 7 || account.Nickname != "测试用户" || account.BalanceUSD != 3 || account.UsedUSD != 0.5 || account.RequestCount != 12 {
		t.Fatalf("account snapshot = %+v", account)
	}
	if err := service.SyncDoge(); err != nil {
		t.Fatalf("manual SyncDoge() error = %v", err)
	}
	if keyCalls != 2 {
		t.Fatalf("manual SyncDoge must refresh the existing key, calls = %d", keyCalls)
	}
	if err := service.syncDoge(context.Background(), "", false, dogeSyncMetadata); err != nil {
		t.Fatalf("metadata refresh = %v", err)
	}
	if keyCalls != 2 {
		t.Fatalf("metadata refresh must not refetch an existing key, calls = %d", keyCalls)
	}
	tokenItems = append(tokenItems, map[string]any{
		"id": 43, "user_id": 7, "key": "new********key", "status": 1,
		"name": "新增令牌", "group": "余额低价组", "unlimited_quota": true,
	})
	if err := service.syncDoge(context.Background(), "", false, dogeSyncMetadata); err != nil {
		t.Fatalf("metadata sync = %v", err)
	}
	if keyCalls != 3 {
		t.Fatalf("metadata sync should fetch only new key, calls = %d", keyCalls)
	}
	if tokens := runtime.State().Config.Doge.Tokens; len(tokens) != 2 || tokens[0].Key != "sk-full-key-42" || tokens[1].Key != "sk-full-key-43" {
		t.Fatalf("metadata key cache = %+v", tokens)
	}
	if err := service.RedeemDoge("fake-redemption-code"); err != nil {
		t.Fatalf("RedeemDoge() error = %v", err)
	}
	if keyCalls != 5 {
		t.Fatalf("full post-redemption sync should refresh both keys, calls = %d", keyCalls)
	}
	if topupMethod != http.MethodPost || string(topupBody) != `{"key":"fake-redemption-code"}` {
		t.Fatalf("topup request = %s %s", topupMethod, topupBody)
	}
	persisted, err := os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), "fake-redemption-code") {
		t.Fatal("redemption code must not be persisted")
	}
}

func TestDogeTokenPermittedUsesCurrentGroupDirectory(t *testing.T) {
	tests := []struct {
		name   string
		token  config.DogeToken
		groups []string
		want   bool
	}{
		{name: "display name", token: config.DogeToken{Group: "余额低价组", GroupDisplayName: "GPT低价组"}, groups: []string{"GPT低价组"}, want: true},
		{name: "raw group fallback", token: config.DogeToken{Group: "余额低价组"}, groups: []string{"余额低价组"}, want: true},
		{name: "raw group with stale display name", token: config.DogeToken{Group: "余额稳定组", GroupDisplayName: "旧展示名"}, groups: []string{"余额稳定组"}, want: true},
		{name: "missing group", token: config.DogeToken{Group: "限时优惠组", GroupDisplayName: "限时优惠组"}, groups: []string{"GPT低价组"}, want: false},
		{name: "empty group", token: config.DogeToken{}, groups: []string{"GPT低价组"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dogeTokenPermitted(test.token, test.groups); got != test.want {
				t.Fatalf("dogeTokenPermitted(%+v, %v) = %v, want %v", test.token, test.groups, got, test.want)
			}
		})
	}
}

func TestParseDogeGroupsKeepsRawKeysForMatchingAndOnlyDisplayNameForUI(t *testing.T) {
	groups, details := parseDogeGroups(map[string]any{
		"余额低价组":  map[string]any{"display_name": "GPT低价组", "ratio": float64(0.02)},
		"稳定国产六折": map[string]any{"desc": "只有国产模型"},
	})

	if !containsString(groups, "余额低价组") || !containsString(groups, "稳定国产六折") {
		t.Fatalf("group permission keys = %#v", groups)
	}
	if details["余额低价组"].DisplayName != "GPT低价组" {
		t.Fatalf("display name = %#v", details["余额低价组"])
	}
	if details["稳定国产六折"].DisplayName != "" {
		t.Fatalf("missing display_name must stay empty, got %#v", details["稳定国产六折"])
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSyncDogeAnnouncementsInitializesReadStateAndDetectsNewItems(t *testing.T) {
	announcements := []map[string]any{{"id": int64(10), "content": "首次公告", "publishDate": "2026-08-22T00:00:00Z", "type": "default"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("public announcement request must not send authorization, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/status":
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"announcements_enabled": true, "announcements": announcements}, "message": "", "success": true})
		case "/api/notice":
			writeDogeTestJSON(t, w, map[string]any{"data": "当前公告", "message": "", "success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Network.Mode = "direct"
	cfg.Doge.BaseURL = server.URL
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	service := NewDesktopService(newTestRuntime(t, directory, store, cfg))

	if err := service.SyncDogeAnnouncements(); err != nil {
		t.Fatalf("initial announcement sync = %v", err)
	}
	state := service.GetState().Doge.Notifications
	if !state.Initialized || state.CurrentNotice != "当前公告" || state.UnreadCount != 0 || len(state.Alerts) != 0 {
		t.Fatalf("initial announcement state = %+v", state)
	}

	announcements = append(announcements, map[string]any{"id": int64(11), "content": "新公告", "publishDate": "2026-08-22T01:00:00Z", "type": "warning"})
	if err := service.SyncDogeAnnouncements(); err != nil {
		t.Fatalf("new announcement sync = %v", err)
	}
	state = service.GetState().Doge.Notifications
	if state.UnreadCount != 1 || len(state.Alerts) != 1 || state.Alerts[0].Kind != NotificationKindAnnouncement || state.Alerts[0].AnnouncementID != 11 {
		t.Fatalf("new announcement state = %+v", state)
	}
	if err := service.DismissDogeNotification(NotificationKindAnnouncement); err != nil {
		t.Fatalf("dismiss announcement = %v", err)
	}
	state = service.GetState().Doge.Notifications
	if state.UnreadCount != 0 || len(state.Alerts) != 0 {
		t.Fatalf("dismissed announcement state = %+v", state)
	}
}

func TestReconcileDogeQuotaAlertRecordsPersistsOneRecordPerIdentity(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	notifications := config.DogeNotificationState{
		BalanceAlertEnabled: true, BalanceAlertThresholdUSD: 1,
		SubscriptionAlertEnabled: true, SubscriptionAlertThresholdUSD: 1,
		DismissedAlertKeys: []string{},
	}
	account := config.DogeAccount{ID: 7, Quota: 400000}
	subscriptions := []config.DogeSubscription{
		{ID: 9, Status: "active", AmountTotal: 400000, EndTime: now.Add(time.Hour).Unix()},
		{ID: 10, Status: "active", AmountTotal: 1500000, EndTime: now.Add(time.Hour).Unix()},
	}

	reconcileDogeQuotaAlertRecords(&notifications, account, subscriptions, now)
	if len(notifications.BalanceAlertRecords) != 1 || notifications.BalanceAlertRecords[0].AccountID != 7 {
		t.Fatalf("balance records after first low sync = %+v", notifications.BalanceAlertRecords)
	}
	if len(notifications.SubscriptionAlertRecords) != 2 || notifications.SubscriptionAlertRecords[0].SubscriptionID != 9 || notifications.SubscriptionAlertRecords[1].SubscriptionID != 10 || notifications.SubscriptionAlertRecords[1].State != subscriptionAlertStateExpiringSoon {
		t.Fatalf("subscription records after first low sync = %+v", notifications.SubscriptionAlertRecords)
	}
	firstNotifiedAt := notifications.BalanceAlertRecords[0].NotifiedAt
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{ID: 7, Quota: 300000}, subscriptions, now.Add(time.Minute))
	if len(notifications.BalanceAlertRecords) != 1 || notifications.BalanceAlertRecords[0].NotifiedAt != firstNotifiedAt {
		t.Fatalf("same low state should keep one notification record = %+v", notifications.BalanceAlertRecords)
	}

	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{ID: 7, Quota: 1500000}, []config.DogeSubscription{
		{ID: 9, Status: "active", AmountTotal: 2000000, EndTime: now.Add(time.Hour).Unix()},
	}, now.Add(2*time.Minute))
	if len(notifications.BalanceAlertRecords) != 0 || len(notifications.SubscriptionAlertRecords) != 1 || notifications.SubscriptionAlertRecords[0].State != subscriptionAlertStateExpiringSoon {
		t.Fatalf("recovered quota should retain only the expiring-soon record = balance:%+v subscription:%+v", notifications.BalanceAlertRecords, notifications.SubscriptionAlertRecords)
	}

	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{ID: 7, Quota: 400000}, []config.DogeSubscription{
		{ID: 9, Status: "expired", AmountTotal: 400000, EndTime: now.Add(-time.Minute).Unix()},
	}, now.Add(3*time.Minute))
	if len(notifications.BalanceAlertRecords) != 1 || notifications.BalanceAlertRecords[0].Acknowledged {
		t.Fatalf("a later low state should create a fresh unacknowledged balance record = %+v", notifications.BalanceAlertRecords)
	}
	if len(notifications.SubscriptionAlertRecords) != 0 {
		t.Fatalf("expired subscription should not keep a record = %+v", notifications.SubscriptionAlertRecords)
	}
}

func TestReconcileDogeQuotaAlertRecordsUsesCustomThresholdAndAccountIdentity(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	notifications := config.DogeNotificationState{
		BalanceAlertEnabled: true, BalanceAlertThresholdUSD: 2.5,
		SubscriptionAlertEnabled: true, SubscriptionAlertThresholdUSD: 3.5,
		DismissedAlertKeys: []string{},
	}
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{ID: 11, Quota: 1000000}, []config.DogeSubscription{
		{ID: 12, Status: "active", AmountTotal: 1500000, EndTime: now.Add(time.Hour).Unix()},
	}, now)
	if len(notifications.BalanceAlertRecords) != 1 || notifications.BalanceAlertRecords[0].ThresholdUSD != 2.5 {
		t.Fatalf("custom balance threshold record = %+v", notifications.BalanceAlertRecords)
	}
	if len(notifications.SubscriptionAlertRecords) != 1 || notifications.SubscriptionAlertRecords[0].ThresholdUSD != 3.5 {
		t.Fatalf("custom subscription threshold record = %+v", notifications.SubscriptionAlertRecords)
	}

	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{ID: 13, Quota: 1000000}, nil, now.Add(time.Minute))
	if len(notifications.BalanceAlertRecords) != 2 || findDogeBalanceAlertRecord(notifications.BalanceAlertRecords, 13) < 0 {
		t.Fatalf("account ID change should create a separate record = %+v", notifications.BalanceAlertRecords)
	}
}

func TestReconcileDogeQuotaAlertRecordsDoesNotCreateDisabledAlerts(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	notifications := config.DogeNotificationState{
		BalanceAlertThresholdUSD: 1, SubscriptionAlertThresholdUSD: 1,
		DismissedAlertKeys: []string{},
	}
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{ID: 7, Quota: 400000}, []config.DogeSubscription{
		{ID: 9, Status: "active", AmountTotal: 400000, EndTime: now.Add(time.Hour).Unix()},
	}, now)
	if len(notifications.BalanceAlertRecords) != 0 || len(notifications.SubscriptionAlertRecords) != 0 {
		t.Fatalf("disabled alerts should not create records = balance:%+v subscription:%+v", notifications.BalanceAlertRecords, notifications.SubscriptionAlertRecords)
	}
}

func TestReconcileDogeSubscriptionLifecycleAndExpiredDismissal(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	notifications := config.DogeNotificationState{
		SubscriptionAlertEnabled: true, SubscriptionAlertThresholdUSD: 1, DismissedAlertKeys: []string{},
	}
	subscription := config.DogeSubscription{ID: 21, PlanID: 8, PlanTitle: "即将到期套餐", Status: "active", AmountTotal: 2000000, AmountUsed: 500000, EndTime: now.Add(12 * time.Hour).Unix()}
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{}, []config.DogeSubscription{subscription}, now)
	if len(notifications.SubscriptionAlertRecords) != 1 || notifications.SubscriptionAlertRecords[0].State != subscriptionAlertStateExpiringSoon {
		t.Fatalf("expiring-soon state = %+v", notifications.SubscriptionAlertRecords)
	}
	notifications.SubscriptionAlertRecords[0].Acknowledged = true
	subscription.Status = "expired"
	subscription.EndTime = now.Add(-time.Minute).Unix()
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{}, []config.DogeSubscription{subscription}, now)
	if len(notifications.SubscriptionAlertRecords) != 1 || notifications.SubscriptionAlertRecords[0].State != subscriptionAlertStateExpired || notifications.SubscriptionAlertRecords[0].Acknowledged {
		t.Fatalf("expired transition should reopen the alert = %+v", notifications.SubscriptionAlertRecords)
	}
	notifications.DismissedAlertKeys = append(notifications.DismissedAlertKeys, subscriptionExpiredAlertKey(subscription.ID))
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{}, []config.DogeSubscription{subscription}, now.Add(time.Minute))
	if len(notifications.SubscriptionAlertRecords) != 1 || !notifications.SubscriptionAlertRecords[0].Acknowledged {
		t.Fatalf("dismissed expired subscription should retain an acknowledged record = %+v", notifications.SubscriptionAlertRecords)
	}
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{}, nil, now.Add(2*time.Minute))
	if len(notifications.SubscriptionAlertRecords) != 0 {
		t.Fatalf("upstream removal should clear the expired subscription record = %+v", notifications.SubscriptionAlertRecords)
	}

	belowThreshold := subscription
	belowThreshold.ID = 22
	belowThreshold.AmountTotal = 400000
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{}, []config.DogeSubscription{belowThreshold}, now)
	if len(notifications.SubscriptionAlertRecords) != 0 {
		t.Fatalf("expired subscription at or below threshold should stay silent = %+v", notifications.SubscriptionAlertRecords)
	}
}

func TestQuotaAlertThresholdUsesStrictlyLessThan(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	notifications := config.DogeNotificationState{
		BalanceAlertEnabled: true, BalanceAlertThresholdUSD: 1,
		SubscriptionAlertEnabled: true, SubscriptionAlertThresholdUSD: 1,
	}
	reconcileDogeQuotaAlertRecords(&notifications, config.DogeAccount{ID: 7, Quota: 500000}, []config.DogeSubscription{{
		ID: 9, Status: "active", AmountTotal: 500000, EndTime: now.Add(time.Hour).Unix(),
	}}, now)
	if len(notifications.BalanceAlertRecords) != 0 || len(notifications.SubscriptionAlertRecords) != 0 {
		t.Fatalf("amount equal to threshold must not alert: balance=%+v subscription=%+v", notifications.BalanceAlertRecords, notifications.SubscriptionAlertRecords)
	}
}

func TestFilterDogeSubscriptionsForStorageUsesUpstreamSnapshotAndSevenDayRetention(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	filtered := filterDogeSubscriptionsForStorage([]config.DogeSubscription{
		{ID: 20, Status: "active", EndTime: now.Add(time.Hour).Unix()},
		{ID: 21, Status: "active", EndTime: now.Add(-7 * 24 * time.Hour).Unix()},
		{ID: 22, Status: "expired", EndTime: now.Add(-7*24*time.Hour - time.Second).Unix()},
		{ID: 23, Status: "disabled", EndTime: now.Add(time.Hour).Unix()},
	}, now)
	if len(filtered) != 2 || filtered[0].ID != 20 || filtered[1].ID != 21 || filtered[1].Status != subscriptionAlertStateExpired {
		t.Fatalf("stored subscription lifecycle = %+v", filtered)
	}
	if omitted := filterDogeSubscriptionsForStorage(nil, now); len(omitted) != 0 {
		t.Fatalf("an upstream omission must not restore an old local subscription: %+v", omitted)
	}
}

func TestAnnouncementMergePreservesSubscriptionDismissalKeys(t *testing.T) {
	notifications := config.DogeNotificationState{
		Initialized:        true,
		DismissedAlertKeys: []string{subscriptionExpiredAlertKey(21), announcementAlertKey(10)},
	}
	snapshot := dogeAnnouncementSnapshot{Status: dogeStatusResponse{AnnouncementsEnabled: true, Announcements: []config.DogeAnnouncement{{ID: 10}}}}
	merged := mergeDogeAnnouncementState(notifications, snapshot)
	if !containsString(merged.DismissedAlertKeys, subscriptionExpiredAlertKey(21)) {
		t.Fatalf("announcement merge removed subscription state: %v", merged.DismissedAlertKeys)
	}
}

func TestReorderDogeTokensUsesRemoteIDIdentity(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Network.Mode = "direct"
	cfg.Doge.Tokens = []config.DogeToken{
		{ID: 41, Name: "会变化的名称", Group: "旧分组", MaskedKey: "aaaa********1111"},
		{ID: 42, Name: "另一个名称", Group: "另一个分组", MaskedKey: "bbbb********2222"},
	}
	cfg.Doge.TokenOrder = []string{"41", "42"}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)

	if err := service.ReorderDogeTokens([]string{"42", "41"}); err != nil {
		t.Fatal(err)
	}
	got := runtime.State().Config.Doge.TokenOrder
	want := []string{"42", "41"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("token order = %v, want %v", got, want)
	}
}

func TestMergeDogeTokenOrderUsesRemoteIDWhenDisplayFieldsChange(t *testing.T) {
	tokens := []config.DogeToken{
		{ID: 41, Name: "新名称 A", Group: "新分组 A", MaskedKey: "new-a********1111"},
		{ID: 42, Name: "新名称 B", Group: "新分组 B", MaskedKey: "new-b********2222"},
		{ID: 43, Name: "新增令牌", Group: "新增分组", MaskedKey: "new-c********3333"},
	}
	got := mergeDogeTokenOrder([]string{"42", "41"}, tokens)
	want := []string{"42", "41", "43"}
	if len(got) != len(want) {
		t.Fatalf("merged order = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("merged order = %v, want %v", got, want)
		}
	}
}

func TestNormalizeDogeAPIKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "raw", input: "123456", want: "sk-123456"},
		{name: "prefixed", input: " sk-123456 ", want: "sk-123456"},
		{name: "empty", input: "  ", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeDogeAPIKey(test.input); got != test.want {
				t.Fatalf("normalizeDogeAPIKey(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
	if isCompleteDogeAPIKey("sk-fake********key") {
		t.Fatal("masked key must not be treated as complete")
	}
	if !isCompleteDogeAPIKey("sk-full-key") {
		t.Fatal("complete key should be accepted")
	}
}

func TestDogeTokenNoteUsesMaskedKeyAndQuota(t *testing.T) {
	if got := dogeTokenNote(config.DogeToken{MaskedKey: "fake********key", UnlimitedQuota: true}); got != "sk-fake********key · 不限额度" {
		t.Fatalf("unlimited note = %q", got)
	}
	if got := dogeTokenNote(config.DogeToken{MaskedKey: "fake********key", RemainQuota: 12345}); got != "sk-fake********key · 剩余 12345" {
		t.Fatalf("limited note = %q", got)
	}
}

func TestEditDogeTokenNormalizesKeyWithoutActivating(t *testing.T) {
	const accessToken = "fake-doge-access-token"
	keyCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/status":
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"announcements_enabled": true, "announcements": []any{}}, "message": "", "success": true})
		case r.URL.Path == "/api/notice":
			writeDogeTestJSON(t, w, map[string]any{"data": "", "message": "", "success": true})
		case r.URL.Path == "/api/user/self":
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"id": 7, "username": "fake-user"}, "message": "", "success": true})
		case r.URL.Path == "/api/user/self/groups":
			writeDogeTestJSON(t, w, map[string]any{"data": []string{"可用分组"}, "message": "", "success": true})
		case r.URL.Path == "/api/subscription/self":
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"subscriptions": []any{}}, "message": "", "success": true})
		case r.URL.Path == "/api/user/topup/info":
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"enable_redemption": true}, "message": "", "success": true})
		case r.URL.Path == "/api/token/" && r.Method == http.MethodGet:
			writeDogeTestJSON(t, w, map[string]any{
				"data": map[string]any{"page": 1, "page_size": 100, "total": 1, "items": []map[string]any{{
					"id": 42, "user_id": 7, "key": "fake********key", "status": 2, "name": "禁用令牌", "group": "已关闭分组",
				}}},
				"message": "", "success": true,
			})
		case r.URL.Path == "/api/token/42/key" && r.Method == http.MethodPost:
			keyCalls++
			writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"key": "123456"}, "message": "", "success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Network.Mode = "direct"
	cfg.Doge.BaseURL = server.URL
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	if err := service.BindDoge(accessToken); err != nil {
		t.Fatalf("BindDoge() error = %v", err)
	}
	boundKeyCalls := keyCalls
	if err := service.SetDogeTokenCategories([]DogeTokenCategoryInput{{ID: 42, Category: config.CategoryImage}}); err != nil {
		t.Fatalf("SetDogeTokenCategories() error = %v", err)
	}
	if err := service.EditDogeToken(42); err != nil {
		t.Fatalf("EditDogeToken() error = %v", err)
	}
	if keyCalls != boundKeyCalls {
		t.Fatalf("editing a locally cached token must not refetch its key: before=%d after=%d", boundKeyCalls, keyCalls)
	}

	state := runtime.State().Config
	if len(state.Profiles) != 1 || state.Profiles[0].APIKey != "sk-123456" || state.Profiles[0].Category != config.CategoryImage || state.Profiles[0].Note != "sk-fake********key · 剩余 0" {
		t.Fatalf("profiles = %+v", state.Profiles)
	}
	if len(state.ActiveProfiles) != 0 {
		t.Fatalf("edit should not activate a disabled token: %v", state.ActiveProfiles)
	}
	if err := service.EnableDogeToken(42); err == nil {
		t.Fatal("EnableDogeToken should reject a token whose group is unavailable")
	}
	if err := service.ActivateProfile(state.Profiles[0].ID); err == nil {
		t.Fatal("ActivateProfile should reject an imported token whose group is unavailable")
	}
}

func TestSyncDogeTokenKeysLimitsConcurrencyToEight(t *testing.T) {
	var mu sync.Mutex
	active, maxActive, calls := 0, 0, 0
	completedFirstBatch := false
	batchViolation := false
	release := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/key") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		active++
		calls++
		if calls > dogeTokenKeyConcurrency && !completedFirstBatch {
			batchViolation = true
		}
		if active > maxActive {
			maxActive = active
		}
		if active == dogeTokenKeyConcurrency {
			releaseOnce.Do(func() { close(release) })
		}
		mu.Unlock()
		<-release
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active--
		if calls == dogeTokenKeyConcurrency && active == 0 {
			completedFirstBatch = true
		}
		mu.Unlock()
		writeDogeTestJSON(t, w, map[string]any{"data": map[string]any{"key": "complete-key"}, "message": "", "success": true})
	}))
	defer server.Close()

	tokens := make([]config.DogeToken, 16)
	for index := range tokens {
		tokens[index] = config.DogeToken{ID: int64(index + 1)}
	}
	service := &DesktopService{}
	client := &http.Client{Transport: http.DefaultTransport, Timeout: 5 * time.Second}
	if err := service.syncDogeTokenKeys(context.Background(), client, server.URL, "fake-access-token", tokens, nil, dogeSyncFull); err != nil {
		t.Fatalf("syncDogeTokenKeys() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != len(tokens) || maxActive != dogeTokenKeyConcurrency || active != 0 || batchViolation {
		t.Fatalf("key concurrency = calls:%d max:%d active:%d batchViolation:%t", calls, maxActive, active, batchViolation)
	}
}

func writeDogeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode test response: %v", err)
	}
}
