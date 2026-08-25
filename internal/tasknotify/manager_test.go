/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project        : CodexRelay
 * @Description    : Codex API 中转热切换桌面工具
 * @File           : 任务通知本地生命周期与耐久投递回归测试
 * @Read me        : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind         : 二次开发请保留原版权信息，谢谢。
 */
package tasknotify

import (
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
)

// 本测试使用脱敏构造的本地 rollout 与 HTTP 接收器，保护首次基线、静默确认、直接 GET 投递和去重行为。
func TestManagerPromotesQuietTerminalEventAndVisitsConfiguredURL(t *testing.T) {
	var mu sync.Mutex
	var requests []struct {
		Method string
		Body   string
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		mu.Lock()
		requests = append(requests, struct {
			Method string
			Body   string
		}{Method: request.Method, Body: string(body)})
		mu.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dataDirectory := t.TempDir()
	sessionsDirectory := filepath.Join(t.TempDir(), "sessions")
	rollout := filepath.Join(sessionsDirectory, "2026", "08", "25", "rollout-test.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := "{\"type\":\"session_meta\",\"payload\":{\"id\":\"thread-test\"}}\n" +
		"{\"type\":\"event_msg\",\"timestamp\":\"2026-08-25T10:00:00Z\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"turn-test\",\"started_at\":1787652000}}\n"
	if err := os.WriteFile(rollout, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := Settings{Enabled: true, WebhookURL: server.URL, Events: EventSettings{TaskCompleted: true}, IdleGraceSeconds: 1, RequestTimeoutSeconds: 2}
	manager := NewManager(func() Settings { return settings }, func() string { return dataDirectory }, nil)
	manager.sessionsDirectory = func() (string, error) { return sessionsDirectory, nil }

	manager.runOnce()
	file, err := os.OpenFile(rollout, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"type\":\"event_msg\",\"timestamp\":\"2026-08-25T11:56:08Z\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"turn-test\",\"started_at\":1787652000,\"completed_at\":1787658968}}\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	manager.runOnce()
	if status := manager.Status(); status.Pending != 1 || status.Outbox != 0 {
		t.Fatalf("quiet terminal status = %+v", status)
	}
	past := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(rollout, past, past); err != nil {
		t.Fatal(err)
	}
	manager.runOnce()
	if status := manager.Status(); status.Pending != 0 || status.Outbox != 0 {
		t.Fatalf("delivered terminal status = %+v", status)
	}
	sentPath := filepath.Join(dataDirectory, stateDirectoryName, "sent", eventKey("thread-test", "turn-test")+".json")
	sentEntry, err := readRecord(sentPath)
	if err != nil {
		t.Fatalf("读取发送收据失败: %v", err)
	}
	if sentEntry.StartedAt != "2026-08-25T10:00:00Z" || sentEntry.OccurredAt != "2026-08-25T11:56:08Z" {
		t.Fatalf("未保存 rollout 生命周期时间: startedAt=%q occurredAt=%q", sentEntry.StartedAt, sentEntry.OccurredAt)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 || requests[0].Method != http.MethodGet || requests[0].Body != "" {
		t.Fatalf("unexpected direct URL requests = %+v", requests)
	}
}

func TestRolloutLifecycleTimesAreParsedFromUnixSeconds(t *testing.T) {
	started := parseRolloutUnixTime([]byte("1787648400"))
	completed := parseRolloutUnixTime([]byte("1787655368"))
	if started.IsZero() || completed.IsZero() {
		t.Fatal("rollout 生命周期时间不应为空")
	}
	if got := formatDuration(started, completed); got != "1小时56分08秒" {
		t.Fatalf("rollout 生命周期耗时 = %q", got)
	}
	if got := parseRolloutTimestamp("2026-08-25T12:16:08Z"); got.IsZero() {
		t.Fatal("rollout 顶层事件时间不应为空")
	}
}

func TestEventKeyUsesThreadAndTurnIdentity(t *testing.T) {
	if eventKey("thread-a", "turn-a") != eventKey("thread-a", "turn-a") {
		t.Fatal("相同线程和回合必须生成相同事件标识")
	}
	if eventKey("thread-a", "turn-a") == eventKey("thread-a", "turn-b") {
		t.Fatal("不同回合不能共用事件标识")
	}
}

// URL 中的两个占位符只替换为编码后的单段消息，不增加额外参数或请求体。
func TestPostEventReplacesAndEscapesMessagePlaceholders(t *testing.T) {
	var received struct {
		Method     string
		Path       string
		RequestURI string
		Query      url.Values
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received.Method = request.Method
		received.Path = request.URL.Path
		received.RequestURI = request.RequestURI
		received.Query = request.URL.Query()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	settings := Settings{
		WebhookURL:            server.URL + "/send?token=placeholder&title={title}&content={content}",
		RequestTimeoutSeconds: 2,
	}
	if err := postEvent(t.Context(), settings, "任务已完成", "当前任务：thread-test 已完成，完成耗时：2分03秒，完成时间：2026-08-25 17:02:31"); err != nil {
		t.Fatal(err)
	}
	if received.Method != http.MethodGet || received.Path != "/send" {
		t.Fatalf("通知 URL 请求错误: %+v", received)
	}
	if !strings.Contains(received.RequestURI, "%20") || strings.Contains(received.RequestURI, "+") {
		t.Fatalf("通知内容中的空格必须编码为 %%20，不能编码为加号: %q", received.RequestURI)
	}
	if received.Query.Get("token") != "placeholder" || received.Query.Get("title") != "任务已完成" || received.Query.Get("content") != "当前任务：thread-test 已完成，完成耗时：2分03秒，完成时间：2026-08-25 17:02:31" {
		t.Fatalf("通知 URL 查询参数错误: %+v", received.Query)
	}
}

func TestMessageForRecordUsesSingleLineChineseContent(t *testing.T) {
	occurred := time.Date(2026, 8, 25, 9, 2, 31, 0, time.UTC)
	started := occurred.Add(-(2*time.Hour + 16*time.Minute + 8*time.Second))
	tests := []struct {
		eventType string
		wantTitle string
		wantText  string
	}{
		{EventTaskCompleted, "【QQ机器人】任务已完成", "当前任务：【QQ机器人】项目的【实现阶段7群生命周期与审核 (2)】已完成，完成耗时：2小时16分08秒"},
		{EventTaskAborted, "【QQ机器人】任务异常中断", "当前任务：【QQ机器人】项目的【实现阶段7群生命周期与审核 (2)】异常中断，原因：interrupted，已运行：2小时16分08秒"},
		{EventTokenRequestFailed, "令牌请求故障", "当前类别：Codex，令牌请求达到故障阈值"},
		{EventTokenAutoSwitched, "令牌已自动切换", "从分组：旧组 切换到分组：新组"},
		{EventTokenAutoSwitchFailed, "令牌自动切换失败", "从分组：旧组 切换到分组：无可用分组"},
	}
	for _, test := range tests {
		entry := record{EventType: test.eventType, ThreadID: "01a037b5-b678-76f1-bcea-570562dc0e65", TaskName: "实现阶段7群生命周期与审核 (2)", ProjectName: "QQ机器人", AbortReason: "interrupted", StartedAt: started.Format(time.RFC3339Nano), OccurredAt: occurred.Format(time.RFC3339Nano), Category: "Codex", FromGroup: "旧组", ToGroup: "新组"}
		if test.eventType == EventTokenRequestFailed {
			entry.FailureKind = "auth"
			entry.FailureCount = 5
			entry.FailureStatus = 401
		}
		if test.eventType == EventTokenAutoSwitchFailed {
			entry.ToGroup = "无可用分组"
		}
		title, content := messageForRecord(entry)
		if title != test.wantTitle || !strings.Contains(content, test.wantText) || strings.Contains(content, "\n") {
			t.Fatalf("事件 %s 文案错误: title=%q content=%q", test.eventType, title, content)
		}
	}
	missingTitle, missingContent := messageForRecord(record{EventType: EventTaskCompleted, ThreadID: "thread-test", OccurredAt: occurred.Format(time.RFC3339Nano)})
	if missingTitle != "任务已完成" || !strings.Contains(missingContent, "当前任务：【未归类】项目的【任务名称未记录】已完成") || !strings.Contains(missingContent, "完成耗时：开始时间未记录") || strings.Contains(missingContent, "thread-test") {
		t.Fatalf("缺少开始时间时文案错误: %q", missingContent)
	}
}

func TestEscapeURLComponentUsesPercent20(t *testing.T) {
	if got := escapeURLComponent("测试 空格  +/"); got != "%E6%B5%8B%E8%AF%95%20%E7%A9%BA%E6%A0%BC%20%20%2B%2F" {
		t.Fatalf("URL 占位符应将空格编码为 %%20、加号编码为 %%2B，实际为 %q", got)
	}
}

// 非 rollout 事件由已确认的本地状态变化写入 outbox；本测试保护事件选择、耐久去重和
// 后台 GET 投递不会回到触发调用栈中执行。
func TestManagerEnqueuesSelectedExternalEvent(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	settings := Settings{Enabled: true, WebhookURL: server.URL, Events: EventSettings{TokenAutoSwitched: true}, RequestTimeoutSeconds: 2}
	dataDirectory := t.TempDir()
	manager := NewManager(func() Settings { return settings }, func() string { return dataDirectory }, nil)
	if err := manager.Enqueue(EventTokenAutoSwitched, "switch-test"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enqueue(EventTokenAutoSwitched, "switch-test"); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.Outbox != 1 {
		t.Fatalf("外部事件未按身份去重: %+v", status)
	}
	manager.runOnce()
	if requests != 1 {
		t.Fatalf("外部事件投递次数 = %d, want 1", requests)
	}
	settings.Events.TokenAutoSwitched = false
	if err := manager.Enqueue(EventTokenAutoSwitched, "disabled-test"); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.Outbox != 0 {
		t.Fatalf("关闭的事件仍进入队列: %+v", status)
	}
}

// 本测试使用脱敏 SQLite fixture，保护 root 的 active goal 门控以及 subagent 候选抑制行为。
func TestManagerUsesCodexSQLiteIdleGates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	home := t.TempDir()
	sessionsDirectory := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	state := openFixtureSQLite(t, filepath.Join(home, "state_5.sqlite"))
	execFixtureSQL(t, state, `
		CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT, source TEXT, thread_source TEXT);
		CREATE TABLE thread_spawn_edges (parent_thread_id TEXT, child_thread_id TEXT, status TEXT);
		INSERT INTO threads (id, rollout_path, source, thread_source) VALUES ('root-thread', '', 'cli', 'cli');
		INSERT INTO threads (id, rollout_path, source, thread_source) VALUES ('subagent-thread', '', 'cli', 'subagent');
	`)
	defer state.Close()
	goals := openFixtureSQLite(t, filepath.Join(home, "goals_1.sqlite"))
	execFixtureSQL(t, goals, `
		CREATE TABLE thread_goals (thread_id TEXT PRIMARY KEY, status TEXT);
		INSERT INTO thread_goals (thread_id, status) VALUES ('root-thread', 'active');
	`)
	defer goals.Close()

	rootRollout := filepath.Join(sessionsDirectory, "rollout-root.jsonl")
	subagentRollout := filepath.Join(sessionsDirectory, "rollout-subagent.jsonl")
	for path, threadID := range map[string]string{rootRollout: "root-thread", subagentRollout: "subagent-thread"} {
		contents := "{\"type\":\"session_meta\",\"payload\":{\"id\":\"" + threadID + "\"}}\n" +
			"{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"turn-" + threadID + "\"}}\n"
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := state.Exec("UPDATE threads SET rollout_path = ? WHERE id = ?", rootRollout, "root-thread"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Exec("UPDATE threads SET rollout_path = ? WHERE id = ?", subagentRollout, "subagent-thread"); err != nil {
		t.Fatal(err)
	}

	settings := Settings{Enabled: true, WebhookURL: server.URL, Events: EventSettings{TaskCompleted: true}, IdleGraceSeconds: 1, RequestTimeoutSeconds: 2}
	manager := NewManager(func() Settings { return settings }, func() string { return t.TempDir() }, nil)
	manager.sessionsDirectory = func() (string, error) { return sessionsDirectory, nil }
	manager.dataDirectory = func() string { return filepath.Join(home, "relay-data") }
	manager.runOnce()
	for path, threadID := range map[string]string{rootRollout: "root-thread", subagentRollout: "subagent-thread"} {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"turn-" + threadID + "\"}}\n"); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(rootRollout, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(subagentRollout, old, old); err != nil {
		t.Fatal(err)
	}
	manager.runOnce()
	if status := manager.Status(); status.Pending != 1 || status.Outbox != 0 || requests != 0 {
		t.Fatalf("SQLite 门控状态错误: %+v requests=%d", status, requests)
	}

	if _, err := goals.Exec("UPDATE thread_goals SET status = 'completed' WHERE thread_id = 'root-thread'"); err != nil {
		t.Fatal(err)
	}
	manager.runOnce()
	if status := manager.Status(); status.Pending != 0 || status.Outbox != 0 || requests != 1 {
		t.Fatalf("root goal 完成后未投递: %+v requests=%d", status, requests)
	}
	if suppressed := jsonFiles(filepath.Join(home, "relay-data", stateDirectoryName, "suppressed")); len(suppressed) != 1 {
		t.Fatalf("subagent 候选未抑制: %v", suppressed)
	}
}
