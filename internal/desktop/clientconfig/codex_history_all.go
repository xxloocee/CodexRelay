package clientconfig

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/storage"
)

type allRepairRow struct {
	Table   string
	ID      string
	Before  sql.NullString
	Host    sql.NullString
	HasHost bool
}
type allRepairManifest struct {
	Version   int
	Directory string
	Target    string
	Files     []repairFile
	Rows      []allRepairRow
	Catalog   []allRepairCatalog
}
type allRepairPlan struct {
	files          []allRepairDiskFile
	rows           []allRepairRow
	catalog        []allRepairCatalog
	target, digest string
	skipped        int
	warnings       []string
}

func allRepairTarget(cfg config.AppConfig, directory string) (string, error) {
	if err := repairCheckHome(directory); err != nil {
		return "", err
	}
	// History belongs to the provider selected by Codex, independently of
	// Relay's saved mode or the authentication method used to reach it.
	data, err := os.ReadFile(filepath.Join(directory, "config.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	value, err := readCodexTOML(data)
	if err != nil {
		return "", err
	}
	target := codexSelectedProvider(value)
	// Built-in and third-party providers are both legitimate history buckets.
	return target, nil
}

func allRepairNonRoot(value any) bool {
	switch v := value.(type) {
	case string:
		text := strings.ToLower(strings.TrimSpace(v))
		if text == "subagent" || text == "internal" || text == "memory_consolidation" || strings.HasPrefix(text, "subagent_") || strings.HasPrefix(text, "internal_") {
			return true
		}
		var decoded any
		if strings.HasPrefix(text, "{") && json.Unmarshal([]byte(v), &decoded) == nil {
			return allRepairNonRoot(decoded)
		}
	case map[string]any:
		for _, key := range []string{"sub_agent", "subagent", "internal"} {
			switch marker := v[key].(type) {
			case bool:
				if marker {
					return true
				}
			case string:
				if strings.TrimSpace(marker) != "" {
					return true
				}
			case map[string]any:
				if len(marker) > 0 {
					return true
				}
			case []any:
				if len(marker) > 0 {
					return true
				}
			case float64:
				return true
			case json.Number:
				return true
			}
		}
	}
	return false
}

func allRepairMeta(data []byte) ([]map[string]any, error) {
	var metas []map[string]any
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte("session_meta")) {
			continue
		}
		var value map[string]any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return nil, errors.New("会话元数据无法解析")
		}
		if value["type"] != "session_meta" {
			continue
		}
		payload, ok := value["payload"].(map[string]any)
		if !ok {
			return nil, errors.New("会话元数据不完整")
		}
		metas = append(metas, payload)
	}
	return metas, nil
}
func rewriteAllRepairJSONL(data []byte, target string) ([]byte, bool, error) {
	segments := bytes.SplitAfter(data, []byte("\n"))
	changed := false
	for i, line := range segments {
		if !bytes.Contains(line, []byte("session_meta")) {
			continue
		}
		var value map[string]any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return nil, false, errors.New("会话元数据无法解析")
		}
		if value["type"] != "session_meta" {
			continue
		}
		payload, ok := value["payload"].(map[string]any)
		if !ok {
			return nil, false, errors.New("会话元数据不完整")
		}
		if provider, ok := payload["model_provider"].(string); ok && provider == target {
			continue
		}
		payload["model_provider"] = target
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, false, err
		}
		if bytes.HasSuffix(line, []byte("\r\n")) {
			encoded = append(encoded, '\r', '\n')
		} else if bytes.HasSuffix(line, []byte("\n")) {
			encoded = append(encoded, '\n')
		}
		segments[i] = encoded
		changed = true
	}
	return bytes.Join(segments, nil), changed, nil
}
func allRepairColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		result[name] = true
	}
	return result, rows.Err()
}
func allRepairDBSnapshot(path, target string, nonRoot map[string]bool) ([]allRepairRow, []string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, []string{"本机会话索引不存在，部分历史可能仍无法显示"}, nil
	} else if err != nil {
		return nil, nil, err
	}
	db, err := openCodexHistoryDatabaseMode(path, "ro")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pair := range [][2]string{{"thread_spawn_edges", "child_thread_id"}, {"agent_job_items", "assigned_thread_id"}} {
		columns, err := allRepairColumns(ctx, db, pair[0])
		if err != nil {
			return nil, nil, err
		}
		if !columns[pair[1]] {
			continue
		}
		rows, err := db.QueryContext(ctx, "SELECT "+pair[1]+" FROM "+pair[0]+" WHERE "+pair[1]+" IS NOT NULL")
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, nil, err
			}
			if id != "" {
				nonRoot[id] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	var result []allRepairRow
	var warnings []string
	for _, table := range []string{"threads", "local_thread_catalog"} {
		columns, err := allRepairColumns(ctx, db, table)
		if err != nil {
			return nil, nil, err
		}
		idColumn, sourceColumn := "id", "source"
		if table == "local_thread_catalog" {
			idColumn, sourceColumn = "thread_id", "source_kind"
		}
		if !columns[idColumn] || !columns["model_provider"] {
			warnings = append(warnings, "部分会话索引缺失或版本不兼容，无法保证所有历史都会显示")
			continue
		}
		sourceExpr, threadSourceExpr, hostExpr := "NULL", "NULL", "NULL"
		if columns[sourceColumn] {
			sourceExpr = sourceColumn
		}
		if columns["thread_source"] {
			threadSourceExpr = "thread_source"
		}
		hasHost := table == "local_thread_catalog" && columns["host_id"]
		localHosts := map[string]bool{}
		if hasHost {
			hostExpr = "host_id"
			hostColumns, err := allRepairColumns(ctx, db, "local_thread_catalog_hosts")
			if err != nil {
				return nil, nil, err
			}
			if hostColumns["host_id"] && hostColumns["host_kind"] {
				hosts, err := db.QueryContext(ctx, "SELECT host_id FROM local_thread_catalog_hosts WHERE LOWER(COALESCE(host_kind,''))='local'")
				if err != nil {
					return nil, nil, err
				}
				for hosts.Next() {
					var id string
					if err := hosts.Scan(&id); err != nil {
						hosts.Close()
						return nil, nil, err
					}
					localHosts[id] = true
				}
				err = hosts.Err()
				hosts.Close()
				if err != nil {
					return nil, nil, err
				}
			} else {
				localHosts["local"] = true
			}
		}
		rows, err := db.QueryContext(ctx, "SELECT "+idColumn+",model_provider,"+sourceExpr+","+threadSourceExpr+","+hostExpr+" FROM "+table+" ORDER BY "+idColumn+","+hostExpr)
		if err != nil {
			return nil, nil, err
		}
		skippedHost := false
		for rows.Next() {
			var id, source, threadSource, host, before sql.NullString
			if err := rows.Scan(&id, &before, &source, &threadSource, &host); err != nil {
				rows.Close()
				return nil, nil, err
			}
			if hasHost && (!host.Valid || !localHosts[host.String]) {
				skippedHost = true
				continue
			}
			if !id.Valid || id.String == "" {
				continue
			}
			if allRepairNonRoot(source.String) || allRepairNonRoot(threadSource.String) {
				nonRoot[id.String] = true
			}
			if !before.Valid || before.String != target {
				result = append(result, allRepairRow{Table: table, ID: id.String, Before: before, Host: host, HasHost: hasHost})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
		if skippedHost {
			warnings = append(warnings, "已跳过远程设备或无法确认归属的会话记录")
		}
	}
	return result, warnings, nil
}
func collectAllRepair(directory, target string) (allRepairPlan, error) {
	plan := allRepairPlan{target: target}
	if err := repairCheckHome(directory); err != nil {
		return plan, err
	}
	nonRoot := map[string]bool{}
	rows, warnings, err := allRepairDBSnapshot(filepath.Join(directory, codexStateDatabaseFilename), target, nonRoot)
	if err != nil {
		return plan, err
	}
	plan.warnings = warnings
	type candidate struct {
		change  allRepairDiskFile
		metas   []map[string]any
		changed bool
	}
	var candidates []candidate
	for _, folder := range []string{"sessions", "archived_sessions"} {
		root := filepath.Join(directory, folder)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) && path == root {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			if err := repairSafePath(path); err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("不支持的会话文件类型")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			metas, err := allRepairMeta(data)
			if err != nil || len(metas) == 0 {
				plan.skipped++
				return nil
			}
			for _, meta := range metas {
				if allRepairNonRoot(meta["source"]) || allRepairNonRoot(meta["thread_source"]) {
					id := stringField(meta, "id")
					if id != "" {
						nonRoot[id] = true
					}
				}
			}
			rewritten, changed, err := rewriteAllRepairJSONL(data, target)
			if err != nil {
				return err
			}
			// Only metadata and fingerprints outlive this iteration.
			candidates = append(candidates, candidate{allRepairDiskFile{path: path, before: sha256Hex(data), after: sha256Hex(rewritten), mode: info.Mode().Perm()}, metas, changed})
			return nil
		})
		if err != nil {
			return plan, err
		}
	}
	skippedIDs := map[string]bool{}
	rollouts := map[string]allRepairRollout{}
	for _, candidate := range candidates {
		skip := false
		anonymousChild := false
		for _, meta := range candidate.metas {
			id := stringField(meta, "id")
			if nonRoot[id] || allRepairNonRoot(meta["source"]) || allRepairNonRoot(meta["thread_source"]) {
				skip = true
				if id != "" {
					skippedIDs[id] = true
				} else {
					anonymousChild = true
				}
			}
		}
		if skip {
			if anonymousChild {
				plan.skipped++
			}
			continue
		}
		ids := map[string]bool{}
		for _, meta := range candidate.metas {
			if id := stringField(meta, "id"); id != "" {
				ids[id] = true
			}
		}
		rollouts[candidate.change.path] = allRepairRollout{file: candidate.change, ids: ids}
		if candidate.changed {
			plan.files = append(plan.files, candidate.change)
		}
	}
	for _, row := range rows {
		if nonRoot[row.ID] {
			skippedIDs[row.ID] = true
			continue
		}
		plan.rows = append(plan.rows, row)
	}
	plan.skipped += len(skippedIDs)
	plan.catalog, err = collectRepairCatalog(directory, target, nonRoot, rollouts)
	if err != nil {
		return plan, err
	}
	if plan.skipped > 0 {
		plan.warnings = append(plan.warnings, "已跳过内部子任务或无法安全识别的记录")
	}
	plan.warnings = append(plan.warnings, "归档会话仍保留在归档中；缺失的历史记录和跨账号的加密内容无法保证恢复")
	sort.Slice(plan.files, func(i, j int) bool { return plan.files[i].path < plan.files[j].path })
	// Retain BUG-014 protection: exact candidate rows (table, key, nullable old
	// provider, host) are bound in addition to on-disk bytes and file candidates.
	base, err := repairHistoryDigest(directory, nil, nil)
	if err != nil {
		return plan, err
	}
	encoded, err := json.Marshal(struct {
		Base, Target string
		Rows         []allRepairRow
		Files        []repairFile
		Catalog      []allRepairCatalog
	}{base, target, plan.rows, allRepairFileFingerprints(directory, plan.files), plan.catalog})
	if err != nil {
		return plan, err
	}
	plan.digest = sha256Hex(encoded)
	return plan, nil
}
func allRepairResult(plan allRepairPlan) CodexHistoryRepairResult {
	ids := map[string]bool{}
	for _, row := range plan.rows {
		ids[row.ID] = true
	}
	for _, row := range plan.catalog {
		ids[row.ID] = true
	}
	return CodexHistoryRepairResult{FileCount: len(plan.files), ThreadCount: len(ids), TargetProvider: plan.target, SkippedCount: plan.skipped, Warning: strings.Join(plan.warnings, "；")}
}
func previewAllCodexHistoryRepair(cfg config.AppConfig) (CodexHistoryRepairResult, error) {
	codexHistoryMigrationMu.Lock()
	defer codexHistoryMigrationMu.Unlock()
	directory, err := repairDirectory(cfg)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	target, err := allRepairTarget(cfg, directory)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	plan, err := collectAllRepair(directory, target)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return CodexHistoryRepairResult{}, err
	}
	for token, ticket := range repairTickets {
		if time.Now().After(ticket.expires) {
			delete(repairTickets, token)
		}
	}
	token := hex.EncodeToString(nonce)
	repairTickets[token] = repairTicket{directory: directory, source: "all", digest: plan.digest, expires: time.Now().Add(10 * time.Minute)}
	result := allRepairResult(plan)
	result.Token = token
	result.Message = "将历史会话归入当前账号或 API 配置；请完全退出 Codex 后执行。预览有效期 10 分钟。"
	return result, nil
}
func updateAllRepairRows(path, target string, rows []allRepairRow, catalog []allRepairCatalog, restore bool) error {
	if len(rows) == 0 && len(catalog) == 0 {
		return nil
	}
	db, err := openCodexHistoryDatabase(path)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The key shape itself must not drift: a catalog upgraded to multi-host
	// storage cannot be safely restored using an old thread_id-only key.
	for _, table := range []string{"threads", "local_thread_catalog"} {
		var hostColumnCount int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='host_id'", table).Scan(&hostColumnCount); err != nil {
			return err
		}
		for _, row := range rows {
			if row.Table == table && row.HasHost != (table == "local_thread_catalog" && hostColumnCount > 0) {
				return errors.New("历史索引主机键结构已变化，拒绝覆盖")
			}
		}
	}
	for _, row := range rows {
		idColumn := "id"
		if row.Table == "local_thread_catalog" {
			idColumn = "thread_id"
		} else if row.Table != "threads" {
			return errors.New("无效历史索引表")
		}
		before, after := any(nil), any(target)
		if row.Before.Valid {
			before = row.Before.String
		}
		if restore {
			before, after = after, before
		}
		query := "UPDATE " + row.Table + " SET model_provider=? WHERE " + idColumn + "=? AND model_provider IS ?"
		args := []any{after, row.ID, before}
		if row.HasHost {
			if row.Table != "local_thread_catalog" || !row.Host.Valid {
				return errors.New("无效 catalog 主机键")
			}
			query += " AND host_id IS ?"
			args = append(args, row.Host.String)
		}
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			if restore && count == 0 {
				// A crash may precede the database transaction, or a previous
				// restore may already have committed it. Accept exactly Before.
				query = "SELECT COUNT(*) FROM " + row.Table + " WHERE " + idColumn + "=?"
				checkArgs := []any{row.ID}
				if row.HasHost {
					query += " AND host_id IS ?"
					checkArgs = append(checkArgs, row.Host.String)
				}
				var keys, unchanged int
				if err := tx.QueryRowContext(ctx, query, checkArgs...).Scan(&keys); err != nil {
					return err
				}
				query += " AND model_provider IS ?"
				checkArgs = append(checkArgs, after)
				if err := tx.QueryRowContext(ctx, query, checkArgs...).Scan(&unchanged); err != nil {
					return err
				}
				if keys == 1 && unchanged == 1 {
					continue
				}
			}
			return errors.New("历史索引候选已变化或键不唯一，已拒绝覆盖")
		}
	}
	if err := updateRepairCatalog(ctx, tx, catalog, restore); err != nil {
		return err
	}
	if err := bumpRepairCatalogRevision(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
func runAllRepair(directory, target string, files []allRepairDiskFile, rows []allRepairRow, catalog []allRepairCatalog, restore bool, checkStopped func() error) error {
	if err := checkStopped(); err != nil {
		return err
	}
	files, err := validateRepairDiskFiles(files, target, restore)
	if err != nil {
		return err
	}
	changed := 0
	rollback := func(cause error) error {
		if err := checkStopped(); err != nil {
			return errors.Join(cause, err)
		}
		for i := changed - 1; i >= 0; i-- {
			file := files[i]
			if err := repairSafePath(file.path); err != nil {
				cause = errors.Join(cause, err)
				continue
			}
			cause = errors.Join(cause, writeRepairDiskFile(file, target, !restore))
		}
		return cause
	}
	for _, file := range files {
		if err := repairSafePath(file.path); err != nil {
			return rollback(err)
		}
		if err := checkStopped(); err != nil {
			return rollback(err)
		}
		if err := writeRepairDiskFile(file, target, restore); err != nil {
			return rollback(err)
		}
		changed++
	}
	if len(rows) > 0 || len(catalog) > 0 {
		path := filepath.Join(directory, codexStateDatabaseFilename)
		if err := repairDatabaseWriteReady(path, checkStopped); err != nil {
			return rollback(err)
		}
		if !restore {
			if err := validateRepairCatalogRollouts(directory, catalog); err != nil {
				return rollback(err)
			}
		}
		if err := updateAllRepairRows(path, target, rows, catalog, restore); err != nil {
			return rollback(err)
		}
	}
	return nil
}
func applyAllCodexHistoryRepair(cfg config.AppConfig, dataDirectory, token string, checkStopped func() error) (CodexHistoryRepairResult, error) {
	codexHistoryMigrationMu.Lock()
	defer codexHistoryMigrationMu.Unlock()
	ticket, ok := repairTickets[token]
	delete(repairTickets, token)
	directory, err := repairDirectory(cfg)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	if !ok || ticket.source != "all" || ticket.directory != directory || time.Now().After(ticket.expires) {
		return CodexHistoryRepairResult{}, errors.New("历史预览已失效，请重新预览")
	}
	if err := checkStopped(); err != nil {
		return CodexHistoryRepairResult{}, err
	}
	unlock, err := repairLock(directory)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	defer unlock()
	target, err := allRepairTarget(cfg, directory)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	plan, err := collectAllRepair(directory, target)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	if plan.digest != ticket.digest {
		return CodexHistoryRepairResult{}, errors.New("配置或历史在预览后发生变化，请重新预览")
	}
	result := allRepairResult(plan)
	if len(plan.files) == 0 && len(plan.rows) == 0 && len(plan.catalog) == 0 {
		result.Message = "没有需要修复的历史"
		return result, nil
	}
	if err := repairSafePath(filepath.Join(dataDirectory, codexHistoryBackupDirectory)); err != nil {
		return result, err
	}
	root, err := createCodexHistoryBackupRoot(dataDirectory)
	if err != nil {
		return result, err
	}
	manifest := allRepairManifest{Version: 3, Directory: directory, Target: target, Rows: plan.rows, Catalog: plan.catalog, Files: allRepairFileFingerprints(directory, plan.files)}
	for i, file := range plan.files {
		if err := historyFileMatches(file, file.before); err != nil {
			return result, err
		}
		original, err := os.ReadFile(file.path)
		if err != nil {
			return result, err
		}
		if sha256Hex(original) != file.before {
			return result, errors.New("会话在备份期间变化，请重新预览")
		}
		plan.files[i].backup = filepath.Join(root, fmt.Sprintf("%d.jsonl", i))
		if err := storage.WriteBytesAtomic(plan.files[i].backup, ".backup-*", original, 0600); err != nil {
			return result, err
		}
	}
	if err := storage.WriteJSONAtomic(filepath.Join(root, "repair-manifest.json"), ".manifest-*", manifest); err != nil {
		return result, err
	}
	result.BackupID = filepath.Base(root)
	if err := runAllRepair(directory, target, plan.files, plan.rows, plan.catalog, false, checkStopped); err != nil {
		return result, fmt.Errorf("历史修复失败（备份 %s）: %w", result.BackupID, err)
	}
	result.Message = "历史会话归属已修复并自动备份，请重新启动 Codex。"
	return result, nil
}

// Called under the common history lock after checking processes and paths.
func restoreAllCodexHistoryRepair(directory, root, backupID string, data []byte, checkStopped func() error) (CodexHistoryRepairResult, error) {
	result := CodexHistoryRepairResult{BackupID: backupID}
	var manifest allRepairManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&manifest); err != nil {
		return result, err
	}
	if (manifest.Version != 2 && manifest.Version != 3) || manifest.Directory != directory || strings.TrimSpace(manifest.Target) == "" {
		return result, errors.New("无效历史备份")
	}
	var files []allRepairDiskFile
	seen := map[string]bool{}
	for i, file := range manifest.Files {
		if filepath.Clean(file.Relative) != file.Relative || filepath.Ext(file.Relative) != ".jsonl" || (!strings.HasPrefix(file.Relative, "sessions"+string(os.PathSeparator)) && !strings.HasPrefix(file.Relative, "archived_sessions"+string(os.PathSeparator))) {
			return result, errors.New("无效会话备份路径")
		}
		path, err := repairInside(directory, file.Relative)
		if err != nil {
			return result, err
		}
		if seen[path] {
			return result, errors.New("重复的会话备份路径")
		}
		seen[path] = true
		backup, err := repairInside(root, fmt.Sprintf("%d.jsonl", i))
		if err != nil {
			return result, err
		}
		files = append(files, allRepairDiskFile{path: path, backup: backup, before: file.Before, after: file.After, mode: os.FileMode(file.Mode)})
	}
	rowKeys := map[string]bool{}
	for _, row := range manifest.Rows {
		if row.Table != "threads" && row.Table != "local_thread_catalog" || row.ID == "" || row.HasHost && (row.Table != "local_thread_catalog" || !row.Host.Valid) {
			return result, errors.New("无效索引备份键")
		}
		encoded, _ := json.Marshal(struct {
			Table, ID string
			Host      sql.NullString
			HasHost   bool
		}{row.Table, row.ID, row.Host, row.HasHost})
		key := string(encoded)
		if rowKeys[key] {
			return result, errors.New("重复的索引备份键")
		}
		rowKeys[key] = true
	}
	if err := runAllRepair(directory, manifest.Target, files, manifest.Rows, manifest.Catalog, true, checkStopped); err != nil {
		return result, err
	}
	ids := map[string]bool{}
	for _, row := range manifest.Rows {
		ids[row.ID] = true
	}
	for _, row := range manifest.Catalog {
		ids[row.ID] = true
	}
	result.FileCount = len(files)
	result.ThreadCount = len(ids)
	result.Message = "已按备份恢复每条历史的原 Provider，备份继续保留。"
	return result, nil
}
