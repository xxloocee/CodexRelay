package tasknotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type deliveryError struct {
	status int
	err    error
}

func (e *deliveryError) Error() string {
	if e.status > 0 {
		return fmt.Sprintf("通知服务返回 HTTP %d", e.status)
	}
	return "通知请求失败"
}

// postEvent 只替换 URL 中字面量 {title}、{content}，并将两者按查询参数编码；不会
// 追加参数、识别第三方协议或写入请求体。没有占位符的 URL 仍按用户填写内容直接访问。

func postEvent(parent context.Context, settings Settings, values ...string) error {
	title, content := "", ""
	if len(values) > 0 {
		title = values[0]
	}
	if len(values) > 1 {
		content = values[1]
	}
	endpointURL := strings.ReplaceAll(settings.WebhookURL, "{title}", escapeURLComponent(title))
	endpointURL = strings.ReplaceAll(endpointURL, "{content}", escapeURLComponent(content))
	endpoint, err := url.Parse(endpointURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		return errors.New("任务通知 URL 无效")
	}
	timeout := time.Duration(settings.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return &deliveryError{err: err}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &deliveryError{status: response.StatusCode}
	}
	return nil
}

// escapeURLComponent 使用查询参数的保留字符集合，并把消息中的普通空格编码为
// RFC 3986 要求的 %20；消息中的字面加号则编码为 %2B，避免被服务端误认为是空格。
func escapeURLComponent(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func applyEventDetails(entry *record, details EventDetails) {
	if entry == nil {
		return
	}
	entry.StartedAt = formatStoredTime(details.StartedAt)
	entry.OccurredAt = formatStoredTime(details.OccurredAt)
	entry.Category = strings.TrimSpace(details.Category)
	entry.FromGroup = strings.TrimSpace(details.FromGroup)
	entry.ToGroup = strings.TrimSpace(details.ToGroup)
	entry.AmountUSD = details.AmountUSD
	entry.ThresholdUSD = details.ThresholdUSD
	entry.FailureKind = strings.TrimSpace(details.FailureKind)
	entry.FailureCount = details.FailureCount
	entry.FailureStatus = details.FailureStatus
	entry.FailureWindowMinutes = details.FailureWindowMinutes
	entry.AbortReason = strings.TrimSpace(details.AbortReason)
}

func formatStoredTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseEventTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// parseRolloutUnixTime 解析 rollout 生命周期字段中的 Unix 秒。该格式来自本机
// rollout JSONL 的 task_started、task_complete 和 turn_aborted 事件；字段缺失或
// 格式无法确认时返回零值，由调用方使用已保存的同一回合时间或事件时间兜底。
func parseRolloutUnixTime(value json.RawMessage) time.Time {
	text := strings.TrimSpace(string(value))
	if text == "" || text == "null" {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

// parseRolloutTimestamp 解析 rollout 顶层事件时间，用于生命周期载荷未提供数值
// 时间时保留同一条事件的真实写入时间。
func parseRolloutTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func formatEventTime(value time.Time) string {
	if value.IsZero() {
		return "时间未记录"
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func formatDuration(started, occurred time.Time) string {
	if started.IsZero() || occurred.IsZero() || occurred.Before(started) {
		return "开始时间未记录"
	}
	seconds := int64(occurred.Sub(started) / time.Second)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	seconds %= 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%d小时%02d分%02d秒", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%d分%02d秒", minutes, seconds)
	default:
		return fmt.Sprintf("%d秒", seconds)
	}
}

func displayGroup(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未分组"
	}
	return value
}

func messageForRecord(entry record) (string, string) {
	occurred := parseEventTime(entry.OccurredAt)
	if occurred.IsZero() {
		occurred = parseEventTime(entry.CreatedAt)
	}
	when := formatEventTime(occurred)
	switch entry.EventType {
	case EventTaskCompleted:
		return taskNotificationTitle(entry, "任务已完成"), fmt.Sprintf("当前任务：【%s】项目的【%s】已完成，完成耗时：%s，完成时间：%s", displayProject(entry), displayTask(entry), formatDuration(parseEventTime(entry.StartedAt), occurred), when)
	case EventTaskAborted:
		return taskNotificationTitle(entry, "任务异常中断"), fmt.Sprintf("当前任务：【%s】项目的【%s】异常中断，原因：%s，已运行：%s，中断时间：%s", displayProject(entry), displayTask(entry), displayAbortReason(entry), formatDuration(parseEventTime(entry.StartedAt), occurred), when)
	case EventTokenRequestFailed:
		return "令牌请求故障", tokenRequestFailureMessage(entry, when)
	case EventTokenAutoSwitched:
		return "令牌已自动切换", fmt.Sprintf("当前类别：%s，已从分组：%s 切换到分组：%s，切换时间：%s", displayGroup(entry.Category), displayGroup(entry.FromGroup), displayGroup(entry.ToGroup), when)
	case EventTokenAutoSwitchFailed:
		return "令牌自动切换失败", fmt.Sprintf("当前类别：%s，尝试从分组：%s 切换到分组：%s，切换结果：没有可用的备用令牌，发生时间：%s", displayGroup(entry.Category), displayGroup(entry.FromGroup), displayGroup(entry.ToGroup), when)
	case EventAccountBalanceLow:
		return "账户余额不足", fmt.Sprintf("账户余额不足，当前余额：$%.2f，提醒阈值：$%.2f，检测时间：%s", entry.AmountUSD, entry.ThresholdUSD, when)
	case EventSubscriptionBalanceLow:
		return "套餐余额不足", fmt.Sprintf("套餐余额不足，当前余额：$%.2f，提醒阈值：$%.2f，检测时间：%s", entry.AmountUSD, entry.ThresholdUSD, when)
	default:
		return "CodexRelay 消息通知", fmt.Sprintf("发生时间：%s", when)
	}
}

func displayProject(entry record) string {
	if value := strings.TrimSpace(entry.ProjectName); value != "" {
		return value
	}
	return "未归类"
}

func displayTask(entry record) string {
	if value := strings.TrimSpace(entry.TaskName); value != "" {
		return value
	}
	return "任务名称未记录"
}

func displayAbortReason(entry record) string {
	if value := strings.TrimSpace(entry.AbortReason); value != "" {
		return value
	}
	return "未记录"
}

func taskNotificationTitle(entry record, suffix string) string {
	if project := strings.TrimSpace(entry.ProjectName); project != "" {
		return fmt.Sprintf("【%s】%s", project, suffix)
	}
	return suffix
}

func tokenRequestFailureMessage(entry record, when string) string {
	kind := "上游异常"
	switch entry.FailureKind {
	case "auth":
		kind = fmt.Sprintf("连续 %d 次返回 HTTP %d", entry.FailureCount, entry.FailureStatus)
	case "upstream":
		status := "5xx 或网络故障"
		if entry.FailureStatus >= 500 {
			status = fmt.Sprintf("最近返回 HTTP %d", entry.FailureStatus)
		}
		kind = fmt.Sprintf("%d 分钟内累计 %d 次%s", entry.FailureWindowMinutes, entry.FailureCount, status)
	}
	return fmt.Sprintf("当前类别：%s，令牌请求达到故障阈值：%s，发生时间：%s", displayGroup(entry.Category), kind, when)
}

func isPermanentDeliveryError(err error) bool {
	var delivery *deliveryError
	return errors.As(err, &delivery) && delivery.status > 0 && ((delivery.status >= 300 && delivery.status < 400) || (delivery.status >= 400 && delivery.status < 500 && delivery.status != http.StatusRequestTimeout && delivery.status != http.StatusTooManyRequests))
}
func safeDeliveryError(err error) string {
	var delivery *deliveryError
	if errors.As(err, &delivery) {
		return delivery.Error()
	}
	return "通知请求失败"
}
func retryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 10 {
		attempts = 10
	}
	delay := time.Second * time.Duration(1<<(attempts-1))
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}
