package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"codexrelay/internal/config"
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

// dogeHTTPError 保留上游 HTTP 状态，允许兼容接口缺少可选管理能力时继续同步。
// Error 文案保持原有格式，避免用户看到内部错误类型变化。
type dogeHTTPError struct {
	StatusCode int
	Message    string
}

func (e *dogeHTTPError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("二狗子请求返回 HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("二狗子请求失败（HTTP %d）: %s", e.StatusCode, e.Message)
}

func isDogeNotFound(err error) bool {
	var statusError *dogeHTTPError
	return errors.As(err, &statusError) && statusError.StatusCode == http.StatusNotFound
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
