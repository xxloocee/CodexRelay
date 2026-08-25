/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex SQLite 状态读取与子代理生命周期回归测试
 * @File          : 任务通知本地状态适配器测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind         : 二次开发请保留原版权信息，谢谢。
 */
package tasknotify

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexStateReaderUsesSQLiteRootGoalAndDescendantState(t *testing.T) {
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	database := openFixtureSQLite(t, filepath.Join(home, "state_5.sqlite"))
	defer database.Close()
	execFixtureSQL(t, database, `
		CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT, source TEXT, thread_source TEXT);
		CREATE TABLE thread_spawn_edges (parent_thread_id TEXT, child_thread_id TEXT, status TEXT);
		INSERT INTO threads (id, rollout_path, source, thread_source) VALUES ('root-thread', '', 'cli', 'cli');
		INSERT INTO threads (id, rollout_path, source, thread_source) VALUES ('child-thread', '', '{"subagent":true}', '');
		INSERT INTO thread_spawn_edges (parent_thread_id, child_thread_id, status) VALUES ('root-thread', 'child-thread', 'running');
	`)
	goals := openFixtureSQLite(t, filepath.Join(home, "goals_1.sqlite"))
	defer goals.Close()
	execFixtureSQL(t, goals, `
		CREATE TABLE thread_goals (thread_id TEXT PRIMARY KEY, status TEXT);
		INSERT INTO thread_goals (thread_id, status) VALUES ('root-thread', 'active');
	`)

	reader := newCodexStateReader(sessions)
	if got := reader.classifyThread("root-thread"); got != threadRoot {
		t.Fatalf("root thread classification = %q", got)
	}
	if got := reader.classifyThread("child-thread"); got != threadSubagent {
		t.Fatalf("child thread classification = %q", got)
	}
	if available, status := reader.goalStatus("root-thread"); !available || status != "active" {
		t.Fatalf("goal status = available:%v status:%q", available, status)
	}
	if available, descendants := reader.descendantThreads("root-thread"); !available || len(descendants) != 1 || descendants[0].ID != "child-thread" {
		t.Fatalf("descendants = available:%v rows:%+v", available, descendants)
	}
}

func TestCodexStateReaderLoadsThreadNameAndLocalProjectName(t *testing.T) {
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	database := openFixtureSQLite(t, filepath.Join(home, "state_5.sqlite"))
	defer database.Close()
	execFixtureSQL(t, database, `
		CREATE TABLE threads (id TEXT PRIMARY KEY, name TEXT, rollout_path TEXT, source TEXT, thread_source TEXT);
		INSERT INTO threads (id, name, rollout_path, source, thread_source) VALUES ('root-thread', '实现阶段7群生命周期与审核 (2)', '', 'cli', 'cli');
	`)
	globalState := `{"thread-project-assignments":{"root-thread":{"projectKind":"local","projectId":"project-1"}},"local-projects":{"project-1":{"id":"project-1","name":"QQ机器人"}}}`
	if err := os.WriteFile(filepath.Join(home, ".codex-global-state.json"), []byte(globalState), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := newCodexStateReader(sessions)
	if got := reader.displayInfo("root-thread"); got.TaskName != "实现阶段7群生命周期与审核 (2)" || got.ProjectName != "QQ机器人" {
		t.Fatalf("展示名称 = %+v", got)
	}
}

func TestCodexStateReaderBlocksOnlyFreshActiveDescendantRollouts(t *testing.T) {
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	rollout := filepath.Join(sessions, "rollout-child.jsonl")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollout, []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"child-turn\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	database := openFixtureSQLite(t, filepath.Join(home, "state_5.sqlite"))
	defer database.Close()
	execFixtureSQL(t, database, `
		CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT, source TEXT, thread_source TEXT);
		CREATE TABLE thread_spawn_edges (parent_thread_id TEXT, child_thread_id TEXT, status TEXT);
		INSERT INTO threads (id, rollout_path, source, thread_source) VALUES ('root-thread', '', 'cli', 'cli');
		INSERT INTO threads (id, rollout_path, source, thread_source) VALUES ('child-thread', 'ROLL_OUT_PATH', 'cli', 'subagent');
		INSERT INTO thread_spawn_edges (parent_thread_id, child_thread_id, status) VALUES ('root-thread', 'child-thread', 'running');
	`)
	if _, err := database.Exec("UPDATE threads SET rollout_path = ? WHERE id = 'child-thread'", rollout); err != nil {
		t.Fatal(err)
	}

	reader := newCodexStateReader(sessions)
	if available, active := reader.activeDescendants("root-thread", time.Now()); !available || active != 1 {
		t.Fatalf("fresh active descendant = available:%v active:%d", available, active)
	}
	if err := os.WriteFile(rollout, []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"child-turn\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-subagentOrphanTimeout - time.Minute)
	if err := os.Chtimes(rollout, old, old); err != nil {
		t.Fatal(err)
	}
	if available, active := reader.activeDescendants("root-thread", time.Now()); !available || active != 0 {
		t.Fatalf("closed stale descendant = available:%v active:%d", available, active)
	}
}

func openFixtureSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Ping(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	return database
}

func execFixtureSQL(t *testing.T, database *sql.DB, statement string) {
	t.Helper()
	if _, err := database.Exec(statement); err != nil {
		t.Fatal(err)
	}
}
