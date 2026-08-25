/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex 本地 SQLite 状态与子代理生命周期读取
 * @File          : 任务通知的 root、goal 和子代理状态适配器
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package tasknotify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sqliteQueryTimeout    = 500 * time.Millisecond
	subagentOrphanTimeout = 30 * time.Minute
	rolloutTailLimit      = 4 * 1024 * 1024
)

type threadClassification string

const (
	threadRoot     threadClassification = "root"
	threadSubagent threadClassification = "subagent"
	threadUnknown  threadClassification = "unknown"
)

type descendantState struct {
	ID     string
	Status string
}

type rolloutLifecycle struct {
	Type       string
	TurnID     string
	ModifiedAt time.Time
	Incomplete bool
	Invalid    bool
}

// threadDisplayInfo 是通知正文允许展示的本地元数据。任务名称来自 threads.name，
// 项目名称来自 Codex 全局状态；两者都缺失时由消息渲染层显示明确的未记录文本，
// 不把 thread UUID 暴露给用户。
type threadDisplayInfo struct {
	TaskName    string
	ProjectName string
}

// codexStateReader 只读取 sessions 所属 Codex 主目录中的 SQLite。数据库打开使用
// mode=ro，连接设置 query_only，任何表结构变化、锁竞争或读取超时都会返回不可用，
// 不会把未知状态当成“没有 goal”或“没有子代理”。
type codexStateReader struct {
	codexHome string
}

func newCodexStateReader(sessionsDirectory string) codexStateReader {
	return codexStateReader{codexHome: filepath.Clean(filepath.Dir(sessionsDirectory))}
}

func (r codexStateReader) stateDatabasePath() string {
	return newestSQLite(filepath.Join(r.codexHome, "state_*.sqlite"), "state_")
}

func (r codexStateReader) goalsDatabasePath() string {
	return newestSQLite(filepath.Join(r.codexHome, "goals_*.sqlite"), "goals_")
}

func newestSQLite(pattern, prefix string) string {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return ""
	}
	type candidate struct {
		path    string
		version int
	}
	var candidates []candidate
	for _, path := range paths {
		if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		name := filepath.Base(path)
		versionText := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".sqlite")
		version, parseErr := strconv.Atoi(versionText)
		if parseErr == nil {
			candidates = append(candidates, candidate{path: path, version: version})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].version != candidates[j].version {
			return candidates[i].version > candidates[j].version
		}
		return candidates[i].path > candidates[j].path
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].path
}

func openSQLiteReadOnly(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("SQLite 文件不存在")
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("SQLite 文件不可读")
	}
	fileURL := "file:" + filepath.ToSlash(path) + "?mode=ro"
	database, err := sql.Open("sqlite", fileURL)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), sqliteQueryTimeout)
	defer cancel()
	if _, err := database.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		database.Close()
		return nil, err
	}
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func sqliteScalar(path, query, parameter string) (bool, string) {
	database, err := openSQLiteReadOnly(path)
	if err != nil {
		return false, ""
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), sqliteQueryTimeout)
	defer cancel()
	var value sql.NullString
	err = database.QueryRowContext(ctx, query, parameter).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return true, ""
	}
	if err != nil {
		return false, ""
	}
	return true, value.String
}

func sqliteRows(path, query, parameter string) (bool, []descendantState) {
	database, err := openSQLiteReadOnly(path)
	if err != nil {
		return false, nil
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), sqliteQueryTimeout)
	defer cancel()
	rows, err := database.QueryContext(ctx, query, parameter)
	if err != nil {
		return false, nil
	}
	defer rows.Close()
	var result []descendantState
	for rows.Next() {
		var item descendantState
		if err := rows.Scan(&item.ID, &item.Status); err != nil {
			return false, nil
		}
		item.ID = strings.TrimSpace(item.ID)
		item.Status = strings.ToLower(strings.TrimSpace(item.Status))
		if item.ID != "" {
			result = append(result, item)
		}
	}
	if err := rows.Err(); err != nil {
		return false, nil
	}
	return true, result
}

func (r codexStateReader) classifyThread(threadID string) threadClassification {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return threadUnknown
	}
	database := r.stateDatabasePath()
	threadAvailable, threadSource := sqliteScalar(database, "SELECT COALESCE(thread_source, '') FROM threads WHERE id = ? LIMIT 1", threadID)
	edgeAvailable, edge := sqliteScalar(database, "SELECT child_thread_id FROM thread_spawn_edges WHERE child_thread_id = ? LIMIT 1", threadID)
	if edgeAvailable && strings.TrimSpace(edge) != "" {
		return threadSubagent
	}
	if threadAvailable && strings.EqualFold(strings.TrimSpace(threadSource), "subagent") {
		return threadSubagent
	}
	if threadAvailable && strings.TrimSpace(threadSource) != "" {
		return threadRoot
	}
	sourceAvailable, source := sqliteScalar(database, "SELECT COALESCE(source, '') FROM threads WHERE id = ? LIMIT 1", threadID)
	if sourceAvailable && strings.TrimSpace(source) != "" {
		if strings.Contains(strings.ToLower(source), "subagent") {
			return threadSubagent
		}
		return threadRoot
	}
	return threadUnknown
}

func (r codexStateReader) goalStatus(threadID string) (bool, string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, ""
	}
	if available, status := sqliteScalar(r.goalsDatabasePath(), "SELECT status FROM thread_goals WHERE thread_id = ? LIMIT 1", threadID); available {
		return true, strings.ToLower(strings.TrimSpace(status))
	}
	if available, status := sqliteScalar(r.stateDatabasePath(), "SELECT status FROM thread_goals WHERE thread_id = ? LIMIT 1", threadID); available {
		return true, strings.ToLower(strings.TrimSpace(status))
	}
	return false, ""
}

func (r codexStateReader) descendantThreads(threadID string) (bool, []descendantState) {
	if strings.TrimSpace(threadID) == "" {
		return false, nil
	}
	database := r.stateDatabasePath()
	tableAvailable, tableName := sqliteScalar(database, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ? LIMIT 1", "thread_spawn_edges")
	if !tableAvailable {
		return false, nil
	}
	if strings.TrimSpace(tableName) == "" {
		return true, nil
	}
	return sqliteRows(database, `
		WITH RECURSIVE descendants(id, status) AS (
			SELECT child_thread_id, status FROM thread_spawn_edges WHERE parent_thread_id = ?
			UNION
			SELECT edge.child_thread_id, edge.status
			FROM thread_spawn_edges AS edge
			JOIN descendants ON edge.parent_thread_id = descendants.id
		)
		SELECT id, COALESCE(status, '') FROM descendants
	`, threadID)
}

func (r codexStateReader) rolloutPath(threadID string) (bool, string) {
	if strings.TrimSpace(threadID) == "" {
		return false, ""
	}
	return sqliteScalar(r.stateDatabasePath(), "SELECT rollout_path FROM threads WHERE id = ? LIMIT 1", threadID)
}

// displayInfo 读取任务和项目展示名称。SQLite 或全局状态暂时不可用时返回空字段，
// 调用方仍可使用队列中已保存的名称和固定的未记录文案完成投递。
func (r codexStateReader) displayInfo(threadID string) threadDisplayInfo {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return threadDisplayInfo{}
	}
	info := threadDisplayInfo{}
	if available, name := sqliteScalar(r.stateDatabasePath(), "SELECT COALESCE(name, '') FROM threads WHERE id = ? LIMIT 1", threadID); available {
		info.TaskName = strings.TrimSpace(name)
	}
	info.ProjectName = r.projectName(threadID)
	return info
}

// projectName 从 Codex 全局状态的真实对象结构解析本地项目名称，不依赖 SQLite
// 中可能为空的 project_id，也不把 workspace 路径当作项目名称。
func (r codexStateReader) projectName(threadID string) string {
	data, err := os.ReadFile(filepath.Join(r.codexHome, ".codex-global-state.json"))
	if err != nil {
		return ""
	}
	var state struct {
		ThreadProjectAssignments map[string]struct {
			ProjectKind string `json:"projectKind"`
			ProjectID   string `json:"projectId"`
		} `json:"thread-project-assignments"`
		LocalProjects map[string]struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"local-projects"`
	}
	if json.Unmarshal(data, &state) != nil {
		return ""
	}
	assignment, ok := state.ThreadProjectAssignments[threadID]
	if !ok || strings.TrimSpace(assignment.ProjectID) == "" || (assignment.ProjectKind != "" && assignment.ProjectKind != "local") {
		return ""
	}
	project, ok := state.LocalProjects[assignment.ProjectID]
	if !ok {
		return ""
	}
	return strings.TrimSpace(project.Name)
}

func (r codexStateReader) activeDescendants(threadID string, now time.Time) (available bool, active int) {
	available, descendants := r.descendantThreads(threadID)
	if !available {
		return false, 0
	}
	for _, child := range descendants {
		if child.Status == "closed" {
			continue
		}
		pathAvailable, path := r.rolloutPath(child.ID)
		if !pathAvailable || strings.TrimSpace(path) == "" {
			active++
			continue
		}
		lifecycle, err := readLatestRolloutLifecycle(path)
		if err != nil {
			active++
			continue
		}
		fresh := lifecycle.ModifiedAt.IsZero() || now.Sub(lifecycle.ModifiedAt) < subagentOrphanTimeout
		if (lifecycle.Incomplete && fresh) || lifecycle.Invalid || (lifecycle.Type == "task_started" && fresh) || (lifecycle.Type == "unknown" && fresh) {
			active++
		}
	}
	return true, active
}

func readLatestRolloutLifecycle(path string) (rolloutLifecycle, error) {
	file, err := os.Open(path)
	if err != nil {
		return rolloutLifecycle{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return rolloutLifecycle{}, err
	}
	result := rolloutLifecycle{Type: "unknown", ModifiedAt: info.ModTime()}
	if info.Size() == 0 {
		return result, nil
	}
	if _, err := file.Seek(-1, io.SeekEnd); err != nil {
		return rolloutLifecycle{}, err
	}
	last := []byte{0}
	if _, err := file.Read(last); err != nil {
		return rolloutLifecycle{}, err
	}
	if last[0] != '\n' {
		result.Incomplete = true
		return result, nil
	}
	window := info.Size()
	if window > rolloutTailLimit {
		window = rolloutTailLimit
	}
	if _, err := file.Seek(-window, io.SeekEnd); err != nil {
		return rolloutLifecycle{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, window))
	if err != nil {
		return rolloutLifecycle{}, err
	}
	if int64(len(data)) < info.Size() {
		if index := strings.IndexByte(string(data), '\n'); index >= 0 {
			data = data[index+1:]
		}
	}
	for index := len(data) - 1; index >= 0; {
		start := index
		for start >= 0 && data[start] != '\n' {
			start--
		}
		line := strings.TrimSpace(string(data[start+1 : index+1]))
		if line != "" {
			var envelope struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal([]byte(line), &envelope) == nil && envelope.Type == "event_msg" {
				var payload struct {
					Type   string `json:"type"`
					TurnID string `json:"turn_id"`
				}
				if json.Unmarshal(envelope.Payload, &payload) == nil && (payload.Type == "task_started" || payload.Type == "task_complete" || payload.Type == "turn_aborted") {
					result.Type, result.TurnID = payload.Type, strings.TrimSpace(payload.TurnID)
					return result, nil
				}
			}
		}
		index = start - 1
	}
	return result, nil
}
