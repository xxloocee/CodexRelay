package clientconfig

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"codexrelay/internal/storage"
	_ "modernc.org/sqlite"
)

const (
	codexRelayModelProviderID   = "codexrelay"
	codexLegacyModelProviderID  = "codex_local_access"
	codexStateDatabaseFilename  = "state_5.sqlite"
	codexHistoryBackupDirectory = "codex-history-backups"
)

var codexHistoryMigrationMu sync.Mutex

type codexHistoryFileChange struct {
	path      string
	original  []byte
	rewritten []byte
	mode      os.FileMode
	modTime   time.Time
	backup    string
}

type codexHistoryDatabaseChange struct {
	path string
	ids  []string
}

// codexHistoryProviderIDForMigration deliberately has a narrow allow-list.
// Official and unrelated third-party sessions must retain their own routing;
// only the provider ID used by the previous Codex local-access integration is
// known to represent the same Relay endpoint.
func codexHistoryProviderIDForMigration(providerID string) string {
	if strings.EqualFold(strings.TrimSpace(providerID), codexLegacyModelProviderID) {
		return codexLegacyModelProviderID
	}
	return ""
}

// migrateCodexHistoryProviderBucket moves sessions from the legacy local
// access provider into the stable CodexRelay provider bucket. It returns a
// rollback function that callers can compose with their config transaction.
// No files are touched when the legacy provider is not present.
func migrateCodexHistoryProviderBucket(codexDirectory, dataDirectory, sourceProviderID string) (func() error, error) {
	sourceProviderID = codexHistoryProviderIDForMigration(sourceProviderID)
	if sourceProviderID == "" {
		return nil, nil
	}
	if strings.TrimSpace(codexDirectory) == "" || !filepath.IsAbs(codexDirectory) {
		return nil, errors.New("Codex 会话目录必须是绝对路径")
	}
	if strings.TrimSpace(dataDirectory) == "" || !filepath.IsAbs(dataDirectory) {
		return nil, errors.New("CodexRelay 数据目录必须是绝对路径")
	}

	codexHistoryMigrationMu.Lock()
	defer codexHistoryMigrationMu.Unlock()

	files, err := collectCodexHistoryFileChanges(codexDirectory, sourceProviderID)
	if err != nil {
		return nil, err
	}
	databases, err := collectCodexHistoryDatabaseChanges(codexDirectory, sourceProviderID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 && len(databases) == 0 {
		return nil, nil
	}

	backupRoot, err := createCodexHistoryBackupRoot(dataDirectory)
	if err != nil {
		return nil, err
	}
	if err := writeCodexHistoryManifest(backupRoot, codexDirectory, sourceProviderID, files, databases); err != nil {
		return nil, err
	}
	if err := backupCodexHistoryFiles(backupRoot, codexDirectory, files); err != nil {
		return nil, err
	}
	if err := backupCodexHistoryDatabases(backupRoot, databases); err != nil {
		return nil, err
	}

	changedFiles := make([]codexHistoryFileChange, 0, len(files))
	changedDatabases := make([]codexHistoryDatabaseChange, 0, len(databases))
	rollback := func() error {
		var rollbackErr error
		for index := len(changedDatabases) - 1; index >= 0; index-- {
			rollbackErr = errors.Join(rollbackErr, rollbackCodexHistoryDatabase(changedDatabases[index], sourceProviderID))
		}
		for index := len(changedFiles) - 1; index >= 0; index-- {
			rollbackErr = errors.Join(rollbackErr, rollbackCodexHistoryFile(changedFiles[index]))
		}
		return rollbackErr
	}

	for index := range files {
		file := files[index]
		if err := ensureCodexHistoryFileUnchanged(file); err != nil {
			_ = rollback()
			return nil, err
		}
		if err := storage.WriteBytesAtomic(file.path, ".codexrelay-session-*.tmp", file.rewritten, file.mode); err != nil {
			_ = rollback()
			return nil, fmt.Errorf("迁移 Codex 会话 %s 失败: %w", filepath.Base(file.path), err)
		}
		changedFiles = append(changedFiles, file)
	}
	for index := range databases {
		database := databases[index]
		if err := migrateCodexHistoryDatabase(database, sourceProviderID); err != nil {
			_ = rollback()
			return nil, err
		}
		changedDatabases = append(changedDatabases, database)
	}
	return rollback, nil
}

func collectCodexHistoryFileChanges(codexDirectory, sourceProviderID string) ([]codexHistoryFileChange, error) {
	var paths []string
	for _, relative := range []string{"sessions", "archived_sessions"} {
		root := filepath.Join(codexDirectory, relative)
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("读取 Codex %s 目录失败: %w", relative, err)
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || filepath.Ext(entry.Name()) != ".jsonl" {
				return nil
			}
			paths = append(paths, path)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("扫描 Codex %s 会话失败: %w", relative, err)
		}
	}
	sort.Strings(paths)
	changes := make([]codexHistoryFileChange, 0)
	for _, path := range paths {
		change, changed, err := readCodexHistoryFileChange(path, sourceProviderID)
		if err != nil {
			return nil, err
		}
		if changed {
			changes = append(changes, change)
		}
	}
	return changes, nil
}

func readCodexHistoryFileChange(path, sourceProviderID string) (codexHistoryFileChange, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return codexHistoryFileChange{}, false, fmt.Errorf("读取 Codex 会话 %s 失败: %w", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() {
		return codexHistoryFileChange{}, false, nil
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return codexHistoryFileChange{}, false, fmt.Errorf("读取 Codex 会话 %s 失败: %w", filepath.Base(path), err)
	}
	rewritten, changed := rewriteCodexHistoryJSONL(original, sourceProviderID)
	if !changed {
		return codexHistoryFileChange{}, false, nil
	}
	return codexHistoryFileChange{path: path, original: original, rewritten: rewritten, mode: info.Mode().Perm(), modTime: info.ModTime()}, true, nil
}

func rewriteCodexHistoryJSONL(data []byte, sourceProviderID string) ([]byte, bool) {
	var builder strings.Builder
	builder.Grow(len(data))
	changed := false
	for len(data) > 0 {
		segment := data
		newline := ""
		if index := bytes.IndexByte(data, '\n'); index >= 0 {
			segment = data[:index]
			newline = "\n"
			data = data[index+1:]
		} else {
			data = nil
		}
		line := string(segment)
		lineEnding := ""
		if strings.HasSuffix(line, "\r") {
			line = strings.TrimSuffix(line, "\r")
			lineEnding = "\r"
		}
		if next, ok := rewriteCodexHistoryMetaLine(line, sourceProviderID); ok {
			builder.WriteString(next)
			builder.WriteString(lineEnding)
			builder.WriteString(newline)
			changed = true
		} else {
			builder.WriteString(string(segment))
			builder.WriteString(newline)
		}
	}
	return []byte(builder.String()), changed
}

func rewriteCodexHistoryMetaLine(line, sourceProviderID string) (string, bool) {
	if !strings.Contains(line, "\"session_meta\"") || !strings.Contains(line, "\"model_provider\"") {
		return "", false
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(line), &value); err != nil || value["type"] != "session_meta" {
		return "", false
	}
	payload, ok := value["payload"].(map[string]any)
	if !ok || strings.TrimSpace(stringField(payload, "model_provider")) != sourceProviderID {
		return "", false
	}
	payload["model_provider"] = codexRelayModelProviderID
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func collectCodexHistoryDatabaseChanges(codexDirectory, sourceProviderID string) ([]codexHistoryDatabaseChange, error) {
	paths := []string{filepath.Join(codexDirectory, codexStateDatabaseFilename)}
	configPath := filepath.Join(codexDirectory, "config.toml")
	configuredSQLiteHome := ""
	if data, err := os.ReadFile(configPath); err == nil {
		if sqliteHome := codexSQLiteHome(string(data)); sqliteHome != "" {
			configuredSQLiteHome = sqliteHome
			paths = append(paths, filepath.Join(sqliteHome, codexStateDatabaseFilename))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取 Codex config.toml 失败: %w", err)
	}
	if configuredSQLiteHome == "" {
		if sqliteHome := strings.TrimSpace(os.Getenv("CODEX_SQLITE_HOME")); sqliteHome != "" {
			paths = append(paths, filepath.Join(resolveCodexUserPath(sqliteHome), codexStateDatabaseFilename))
		}
	}
	unique := make(map[string]struct{}, len(paths))
	changes := make([]codexHistoryDatabaseChange, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if _, ok := unique[path]; ok {
			continue
		}
		unique[path] = struct{}{}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("检查 Codex 状态数据库失败: %w", err)
		}
		ids, err := codexHistoryDatabaseIDs(path, sourceProviderID)
		if err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			changes = append(changes, codexHistoryDatabaseChange{path: path, ids: ids})
		}
	}
	return changes, nil
}

func codexSQLiteHome(configText string) string {
	return resolveCodexUserPath(strings.TrimSpace(tomlTopLevelValue(configText, "sqlite_home")))
}

func resolveCodexUserPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return raw
	}
	if raw == "~" {
		return home
	}
	if strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, `~\`) {
		return filepath.Join(home, raw[2:])
	}
	return filepath.Clean(raw)
}

func codexHistoryDatabaseIDs(path, sourceProviderID string) ([]string, error) {
	database, err := openCodexHistoryDatabase(path)
	if err != nil {
		return nil, fmt.Errorf("打开 Codex 状态数据库失败: %w", err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var tableCount int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'threads'").Scan(&tableCount); err != nil {
		return nil, fmt.Errorf("检查 Codex 状态数据库结构失败: %w", err)
	}
	if tableCount == 0 {
		return nil, nil
	}
	var columnCount int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('threads') WHERE name = 'model_provider'").Scan(&columnCount); err != nil {
		return nil, fmt.Errorf("检查 Codex 状态数据库字段失败: %w", err)
	}
	if columnCount == 0 {
		return nil, nil
	}
	rows, err := database.QueryContext(ctx, "SELECT id FROM threads WHERE model_provider = ? ORDER BY id", sourceProviderID)
	if err != nil {
		return nil, fmt.Errorf("读取 Codex 会话 provider 失败: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("读取 Codex 会话 ID 失败: %w", err)
		}
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取 Codex 会话 provider 失败: %w", err)
	}
	return ids, nil
}

func openCodexHistoryDatabase(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=rw")
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func createCodexHistoryBackupRoot(dataDirectory string) (string, error) {
	parent := filepath.Join(filepath.Clean(dataDirectory), codexHistoryBackupDirectory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("创建 Codex 会话备份目录失败: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")
	for index := 1; index < 1000; index++ {
		name := stamp
		if index > 1 {
			name = fmt.Sprintf("%s-%d", stamp, index)
		}
		root := filepath.Join(parent, name)
		if err := os.Mkdir(root, 0o700); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("创建 Codex 会话备份批次失败: %w", err)
		}
		return root, nil
	}
	return "", errors.New("创建 Codex 会话备份批次失败：重试次数过多")
}

func writeCodexHistoryManifest(root, codexDirectory, sourceProviderID string, files []codexHistoryFileChange, databases []codexHistoryDatabaseChange) error {
	manifest := struct {
		CodexDirectory string   `json:"codexDirectory"`
		SourceProvider string   `json:"sourceProvider"`
		TargetProvider string   `json:"targetProvider"`
		Files          []string `json:"files"`
		Databases      []string `json:"databases"`
	}{CodexDirectory: codexDirectory, SourceProvider: sourceProviderID, TargetProvider: codexRelayModelProviderID}
	for _, file := range files {
		manifest.Files = append(manifest.Files, file.path)
	}
	for _, database := range databases {
		manifest.Databases = append(manifest.Databases, database.path)
	}
	return storage.WriteJSONAtomic(filepath.Join(root, "manifest.json"), ".manifest-*.tmp", manifest)
}

func backupCodexHistoryFiles(root, codexDirectory string, files []codexHistoryFileChange) error {
	for index := range files {
		relative, err := filepath.Rel(codexDirectory, files[index].path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("Codex 会话备份路径无效: %s", files[index].path)
		}
		backup := filepath.Join(root, "jsonl", relative)
		if err := storage.WriteBytesAtomic(backup, ".session-backup-*.tmp", files[index].original, 0o600); err != nil {
			return fmt.Errorf("备份 Codex 会话 %s 失败: %w", filepath.Base(files[index].path), err)
		}
		files[index].backup = backup
	}
	return nil
}

func backupCodexHistoryDatabases(root string, databases []codexHistoryDatabaseChange) error {
	for index, database := range databases {
		// Multiple Codex SQLite homes can contain a state_5.sqlite. Prefix the
		// backup with its stable batch index so one source cannot overwrite
		// another while the manifest retains the original absolute paths.
		baseName := fmt.Sprintf("%02d-%s", index+1, filepath.Base(database.path))
		for _, suffix := range []string{"", "-wal", "-shm"} {
			path := database.path + suffix
			data, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("备份 Codex 状态数据库失败: %w", err)
			}
			name := baseName + suffix
			if err := storage.WriteBytesAtomic(filepath.Join(root, "state", name), ".state-backup-*.tmp", data, 0o600); err != nil {
				return fmt.Errorf("备份 Codex 状态数据库失败: %w", err)
			}
		}
	}
	return nil
}

func ensureCodexHistoryFileUnchanged(file codexHistoryFileChange) error {
	info, err := os.Stat(file.path)
	if err != nil {
		return fmt.Errorf("Codex 会话 %s 在迁移期间不可用: %w", filepath.Base(file.path), err)
	}
	data, err := os.ReadFile(file.path)
	if err != nil {
		return fmt.Errorf("读取 Codex 会话 %s 失败: %w", filepath.Base(file.path), err)
	}
	if info.ModTime() != file.modTime || info.Size() != int64(len(file.original)) || !strings.EqualFold(sha256Hex(data), sha256Hex(file.original)) {
		return fmt.Errorf("Codex 会话 %s 在迁移期间被其他进程修改", filepath.Base(file.path))
	}
	return nil
}

func migrateCodexHistoryDatabase(change codexHistoryDatabaseChange, sourceProviderID string) error {
	database, err := openCodexHistoryDatabase(change.path)
	if err != nil {
		return fmt.Errorf("打开 Codex 状态数据库失败: %w", err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("锁定 Codex 状态数据库失败: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, "UPDATE threads SET model_provider = ? WHERE id = ? AND model_provider = ?")
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("准备 Codex 会话迁移失败: %w", err)
	}
	for _, id := range change.ids {
		result, err := stmt.ExecContext(ctx, codexRelayModelProviderID, id, sourceProviderID)
		if err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("迁移 Codex 会话 provider 失败: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			_ = stmt.Close()
			_ = tx.Rollback()
			if err != nil {
				return fmt.Errorf("确认 Codex 会话 provider 迁移失败: %w", err)
			}
			return fmt.Errorf("Codex 会话 %s 在迁移期间被其他进程修改", id)
		}
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("关闭 Codex 会话迁移语句失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Codex 会话 provider 迁移失败: %w", err)
	}
	return nil
}

func rollbackCodexHistoryDatabase(change codexHistoryDatabaseChange, sourceProviderID string) error {
	database, err := openCodexHistoryDatabase(change.path)
	if err != nil {
		return fmt.Errorf("回退 Codex 状态数据库失败: %w", err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("锁定 Codex 状态数据库失败: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, "UPDATE threads SET model_provider = ? WHERE id = ? AND model_provider = ?")
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("准备 Codex 会话回退失败: %w", err)
	}
	for _, id := range change.ids {
		result, err := stmt.ExecContext(ctx, sourceProviderID, id, codexRelayModelProviderID)
		if err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("回退 Codex 会话 provider 失败: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			_ = stmt.Close()
			_ = tx.Rollback()
			if err != nil {
				return fmt.Errorf("确认 Codex 会话 provider 回退失败: %w", err)
			}
			return fmt.Errorf("Codex 会话 %s 在回退期间被其他进程修改", id)
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Codex 会话 provider 回退失败: %w", err)
	}
	return nil
}

func rollbackCodexHistoryFile(file codexHistoryFileChange) error {
	if err := ensureCodexHistoryFileContents(file.path, file.rewritten, file.mode); err != nil {
		return err
	}
	return storage.WriteBytesAtomic(file.path, ".codexrelay-session-rollback-*.tmp", file.original, file.mode)
}

func ensureCodexHistoryFileContents(path string, expected []byte, mode os.FileMode) error {
	data, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if err != nil || statErr != nil || !bytes.Equal(data, expected) {
		return fmt.Errorf("Codex 会话 %s 在回退期间被其他进程修改", filepath.Base(path))
	}
	if mode != 0 && !clientFileModesEqual(info.Mode(), mode) {
		return fmt.Errorf("Codex 会话 %s 权限在回退期间被其他进程修改", filepath.Base(path))
	}
	return nil
}
