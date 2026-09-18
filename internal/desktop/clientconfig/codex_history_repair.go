package clientconfig

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/storage"
)

type CodexHistoryRepairResult struct {
	Token          string `json:"token"`
	FileCount      int    `json:"fileCount"`
	ThreadCount    int    `json:"threadCount"`
	BackupID       string `json:"backupID"`
	Message        string `json:"message"`
	TargetProvider string `json:"targetProvider"`
	SkippedCount   int    `json:"skippedCount"`
	Warning        string `json:"warning"`
}
type repairTicket struct {
	directory, source, digest string
	expires                   time.Time
}

var repairTickets = map[string]repairTicket{} // guarded by codexHistoryMigrationMu
type repairFile struct {
	Relative, Before, After string
	Mode                    uint32
}
type repairDatabase struct {
	Relative string
	IDs      []string
}
type repairManifest struct {
	Version           int
	Directory, Source string
	Files             []repairFile
	Databases         []repairDatabase
}

// Reject symlinks in every ancestor, including Windows junctions as exposed by
// EvalSymlinks. No repair is allowed to traverse outside the selected home.
func repairSafePath(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("历史修复路径必须为绝对路径")
	}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("历史修复不支持符号链接路径")
			}
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil || filepath.Clean(resolved) != current {
				return errors.New("历史修复不支持重定向目录")
			}
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}
func repairInside(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("无效的历史备份路径")
	}
	path := filepath.Join(root, relative)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("历史备份路径越界")
	}
	return path, repairSafePath(path)
}
func repairDirectory(cfg config.AppConfig) (string, error) {
	definition, _ := clientDefinitionFor(config.CategoryCodex)
	directory, _ := clientConfigPath(definition, cfg.ClientConfigs[config.CategoryCodex])
	return directory, repairSafePath(directory)
}
func repairCheckHome(directory string) error {
	if err := repairSafePath(directory); err != nil {
		return err
	}
	for _, relative := range []string{"sessions", "archived_sessions"} {
		root := filepath.Join(directory, relative)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) && path == root {
				return nil
			}
			if err != nil {
				return err
			}
			return repairSafePath(path)
		})
		if err != nil {
			return err
		}
	}
	for _, name := range []string{"config.toml", "auth.json", codexStateDatabaseFilename, codexStateDatabaseFilename + "-wal", codexStateDatabaseFilename + "-shm"} {
		if err := repairSafePath(filepath.Join(directory, name)); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, "config.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	value, err := readCodexTOML(data)
	if err != nil {
		return err
	}
	homes := []string{stringField(value, "sqlite_home"), os.Getenv("CODEX_SQLITE_HOME")}
	if profiles, ok := value["profiles"].(map[string]any); ok {
		for _, profile := range profiles {
			if fields, ok := profile.(map[string]any); ok {
				homes = append(homes, stringField(fields, "sqlite_home"))
			}
		}
	}
	for _, raw := range homes {
		sqliteHome := resolveCodexUserPath(raw)
		if sqliteHome != "" && filepath.Clean(sqliteHome) != filepath.Clean(directory) {
			return errors.New("历史修复暂不支持外置 sqlite_home，请使用 Codex 配置目录内的状态数据库")
		}
	}
	return nil
}
func repairCollect(directory, source string) ([]codexHistoryFileChange, []codexHistoryDatabaseChange, string, error) {
	if err := repairCheckHome(directory); err != nil {
		return nil, nil, "", err
	}
	files, err := collectCodexHistoryFileChanges(directory, source)
	if err != nil {
		return nil, nil, "", err
	}
	databases, err := collectCodexHistoryDatabaseChanges(directory, source)
	if err != nil {
		return nil, nil, "", err
	}
	digest, err := repairHistoryDigest(directory, files, databases)
	return files, databases, digest, err
}

func repairHistoryDigest(directory string, files []codexHistoryFileChange, databases []codexHistoryDatabaseChange) (string, error) {
	hashes := map[string]string{}
	for _, file := range files {
		hashes[file.path] = sha256Hex(file.original)
	}
	for _, name := range []string{"config.toml", "auth.json", codexStateDatabaseFilename, codexStateDatabaseFilename + "-wal"} {
		hash, err := historyFileHash(filepath.Join(directory, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil {
			hashes[name] = hash
		} else {
			hashes[name] = "missing"
		}
	}
	// A writer can change the index between the ID query and byte hashing.
	// Bind the exact queried candidates too, so apply cannot silently broaden
	// the preview. Database IDs are returned in SQL ORDER BY id order.
	candidates := make(map[string][]string, len(databases))
	for _, database := range databases {
		candidates[database.path] = database.ids
	}
	encoded, _ := json.Marshal(struct {
		Hashes             map[string]string
		DatabaseCandidates map[string][]string
	}{hashes, candidates})
	return sha256Hex(encoded), nil
}
func repairSource(source string) bool {
	return source == "openai" || source == codexLegacyModelProviderID
}

func repairDatabaseWriteReady(path string, checkStopped func() error) error {
	if err := checkStopped(); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := repairSafePath(path + suffix); err != nil {
			return err
		}
	}
	return nil
}
func repairResult(files []codexHistoryFileChange, dbs []codexHistoryDatabaseChange) CodexHistoryRepairResult {
	result := CodexHistoryRepairResult{FileCount: len(files)}
	for _, db := range dbs {
		result.ThreadCount += len(db.ids)
	}
	return result
}
func PreviewCodexHistoryRepair(cfg config.AppConfig, source string) (CodexHistoryRepairResult, error) {
	if source == "all" {
		return previewAllCodexHistoryRepair(cfg)
	}
	codexHistoryMigrationMu.Lock()
	defer codexHistoryMigrationMu.Unlock()
	if !repairSource(source) {
		return CodexHistoryRepairResult{}, errors.New("不支持的历史 provider")
	}
	directory, err := repairDirectory(cfg)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	files, dbs, digest, err := repairCollect(directory, source)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return CodexHistoryRepairResult{}, err
	}
	for token, ticket := range repairTickets {
		if time.Now().After(ticket.expires) {
			delete(repairTickets, token)
		}
	}
	token := hex.EncodeToString(nonce)
	repairTickets[token] = repairTicket{directory, source, digest, time.Now().Add(10 * time.Minute)}
	result := repairResult(files, dbs)
	result.Token = token
	result.Message = "预览有效期 10 分钟；执行前请完全退出 Codex，历史将改为使用 Relay。"
	return result, nil
}
func repairLock(directory string) (func(), error) {
	path := filepath.Join(directory, ".codexrelay-history-repair.lock")
	if err := repairSafePath(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockHistoryFile(file); err != nil {
		_ = file.Close()
		return nil, errors.New("其他进程正在修复历史，请稍后重试")
	}
	// Keep the inode/name stable; unlinking after unlock lets another process
	// lock a different inode. OS locks are released even after a process crash.
	return func() { _ = file.Close() }, nil
}
func ApplyCodexHistoryRepair(cfg config.AppConfig, dataDirectory, source, token string) (CodexHistoryRepairResult, error) {
	return applyCodexHistoryRepair(cfg, dataDirectory, source, token, ensureCodexHistoryProcessesStopped)
}

func applyCodexHistoryRepair(cfg config.AppConfig, dataDirectory, source, token string, checkStopped func() error) (CodexHistoryRepairResult, error) {
	if source == "all" {
		return applyAllCodexHistoryRepair(cfg, dataDirectory, token, checkStopped)
	}
	codexHistoryMigrationMu.Lock()
	defer codexHistoryMigrationMu.Unlock()
	ticket, ok := repairTickets[token]
	delete(repairTickets, token)
	directory, err := repairDirectory(cfg)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	if !ok || !repairSource(source) || ticket.source != source || ticket.directory != directory || time.Now().After(ticket.expires) {
		return CodexHistoryRepairResult{}, errors.New("历史预览已失效，请重新预览")
	}
	if strings.TrimSpace(cfg.ActiveProfiles[config.CategoryCodex]) == "" {
		return CodexHistoryRepairResult{}, errors.New("请先启用 Codex Relay 上游，再修复历史")
	}
	if err := checkStopped(); err != nil {
		return CodexHistoryRepairResult{}, err
	}
	unlock, err := repairLock(directory)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	defer unlock()
	definition, _ := clientDefinitionFor(config.CategoryCodex)
	matches, err := clientConfigurationMatches(definition, directory, filepath.Join(directory, "config.toml"), clientProxyURL(cfg, config.CategoryCodex), cfg.LocalAccessToken, "", false)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	if !matches {
		return CodexHistoryRepairResult{}, errors.New("请先成功切换到 Relay 配置，再修复历史")
	}
	files, dbs, digest, err := repairCollect(directory, source)
	if err != nil {
		return CodexHistoryRepairResult{}, err
	}
	if digest != ticket.digest {
		return CodexHistoryRepairResult{}, errors.New("配置或历史在预览后发生变化，请重新预览")
	}
	result := repairResult(files, dbs)
	if len(files) == 0 && len(dbs) == 0 {
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
	manifest := repairManifest{Version: 1, Directory: directory, Source: source}
	for i, file := range files {
		relative, _ := filepath.Rel(directory, file.path)
		manifest.Files = append(manifest.Files, repairFile{relative, sha256Hex(file.original), sha256Hex(file.rewritten), uint32(file.mode)})
		if err := storage.WriteBytesAtomic(filepath.Join(root, fmt.Sprintf("%d.jsonl", i)), ".backup-*", file.original, 0600); err != nil {
			return result, err
		}
	}
	for _, db := range dbs {
		relative, _ := filepath.Rel(directory, db.path)
		manifest.Databases = append(manifest.Databases, repairDatabase{relative, db.ids})
	}
	if err := storage.WriteJSONAtomic(filepath.Join(root, "repair-manifest.json"), ".manifest-*", manifest); err != nil {
		return result, err
	}
	result.BackupID = filepath.Base(root)
	changedFiles, changedDB := 0, 0
	rollback := func(cause error) error {
		if err := checkStopped(); err != nil {
			return fmt.Errorf("历史修复未能自动回退（备份 %s）: %w", result.BackupID, errors.Join(cause, err))
		}
		for i := changedDB - 1; i >= 0; i-- {
			if err := repairDatabaseWriteReady(dbs[i].path, checkStopped); err != nil {
				cause = errors.Join(cause, err)
				continue
			}
			cause = errors.Join(cause, rollbackCodexHistoryDatabase(dbs[i], source))
		}
		for i := changedFiles - 1; i >= 0; i-- {
			if err := repairSafePath(files[i].path); err != nil {
				cause = errors.Join(cause, err)
				continue
			}
			cause = errors.Join(cause, rollbackCodexHistoryFile(files[i]))
		}
		return fmt.Errorf("历史修复失败（备份 %s）: %w", result.BackupID, cause)
	}
	if err := checkStopped(); err != nil {
		return result, rollback(err)
	}
	for _, file := range files {
		if err := repairSafePath(file.path); err != nil {
			return result, rollback(err)
		}
		if err := ensureCodexHistoryFileUnchanged(file); err != nil {
			return result, rollback(err)
		}
		if err := storage.WriteBytesAtomic(file.path, ".repair-*", file.rewritten, file.mode); err != nil {
			return result, rollback(err)
		}
		changedFiles++
	}
	for _, db := range dbs {
		if err := repairDatabaseWriteReady(db.path, checkStopped); err != nil {
			return result, rollback(err)
		}
		if err := migrateCodexHistoryDatabase(db, source); err != nil {
			return result, rollback(err)
		}
		changedDB++
	}
	result.Message = "历史 provider 已修复；请重新启动 Codex。备份 ID 可用于恢复。"
	return result, nil
}

func RestoreCodexHistoryRepair(cfg config.AppConfig, dataDirectory, backupID string) (CodexHistoryRepairResult, error) {
	return restoreCodexHistoryRepair(cfg, dataDirectory, backupID, ensureCodexHistoryProcessesStopped)
}

func restoreCodexHistoryRepair(cfg config.AppConfig, dataDirectory, backupID string, checkStopped func() error) (CodexHistoryRepairResult, error) {
	codexHistoryMigrationMu.Lock()
	defer codexHistoryMigrationMu.Unlock()
	result := CodexHistoryRepairResult{BackupID: backupID}
	if backupID == "" || filepath.Base(backupID) != backupID || strings.ContainsAny(backupID, "/\\:") || backupID == ".." {
		return result, errors.New("无效备份 ID")
	}
	directory, err := repairDirectory(cfg)
	if err != nil {
		return result, err
	}
	if err := checkStopped(); err != nil {
		return result, err
	}
	unlock, err := repairLock(directory)
	if err != nil {
		return result, err
	}
	defer unlock()
	if err := repairCheckHome(directory); err != nil {
		return result, err
	}
	root := filepath.Join(dataDirectory, codexHistoryBackupDirectory, backupID)
	manifestPath := filepath.Join(root, "repair-manifest.json")
	if err := repairSafePath(manifestPath); err != nil {
		return result, err
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return result, err
	}
	var manifest repairManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return result, err
	}
	if manifest.Version == 2 || manifest.Version == 3 {
		return restoreAllCodexHistoryRepair(directory, root, backupID, data, checkStopped)
	}
	if manifest.Version != 1 || manifest.Directory != directory || !repairSource(manifest.Source) {
		return result, errors.New("备份不属于当前 Codex 目录或版本不受支持")
	}
	files := make([]codexHistoryFileChange, 0, len(manifest.Files))
	dbs := make([]codexHistoryDatabaseChange, 0, len(manifest.Databases))
	for i, file := range manifest.Files {
		if filepath.Clean(file.Relative) != file.Relative || filepath.Ext(file.Relative) != ".jsonl" {
			return result, errors.New("备份中的历史路径必须为规范 JSONL 路径")
		}
		path, err := repairInside(directory, file.Relative)
		if err != nil {
			return result, err
		}
		if !strings.HasPrefix(file.Relative, "sessions"+string(os.PathSeparator)) && !strings.HasPrefix(file.Relative, "archived_sessions"+string(os.PathSeparator)) {
			return result, errors.New("备份中的历史路径无效")
		}
		backup, err := repairInside(root, fmt.Sprintf("%d.jsonl", i))
		if err != nil {
			return result, err
		}
		original, err := os.ReadFile(backup)
		if err != nil {
			return result, err
		}
		rewritten, changed := rewriteCodexHistoryJSONL(original, manifest.Source)
		if !changed || sha256Hex(original) != file.Before || sha256Hex(rewritten) != file.After {
			return result, errors.New("历史备份完整性校验失败")
		}
		if err := ensureCodexHistoryFileContents(path, rewritten, os.FileMode(file.Mode)); err != nil {
			return result, err
		}
		files = append(files, codexHistoryFileChange{path: path, original: original, rewritten: rewritten, mode: os.FileMode(file.Mode)})
	}
	for _, db := range manifest.Databases {
		if db.Relative != codexStateDatabaseFilename {
			return result, errors.New("无效的历史数据库路径")
		}
		path, err := repairInside(directory, db.Relative)
		if err != nil {
			return result, err
		}
		ids, err := codexHistoryDatabaseIDs(path, codexRelayModelProviderID)
		if err != nil {
			return result, err
		}
		present := map[string]bool{}
		for _, id := range ids {
			present[id] = true
		}
		for _, id := range db.IDs {
			if !present[id] {
				return result, errors.New("历史数据库已变化，拒绝覆盖，请保留备份手动处理")
			}
		}
		dbs = append(dbs, codexHistoryDatabaseChange{path: path, ids: db.IDs})
	}
	restoredFiles, restoredDB := 0, 0
	rollback := func(cause error) error {
		if err := checkStopped(); err != nil {
			return errors.Join(cause, err)
		}
		for i := restoredDB - 1; i >= 0; i-- {
			if err := repairDatabaseWriteReady(dbs[i].path, checkStopped); err != nil {
				cause = errors.Join(cause, err)
				continue
			}
			cause = errors.Join(cause, migrateCodexHistoryDatabase(dbs[i], manifest.Source))
		}
		for i := restoredFiles - 1; i >= 0; i-- {
			file := files[i]
			if err := repairSafePath(file.path); err != nil {
				cause = errors.Join(cause, err)
				continue
			}
			if err := ensureCodexHistoryFileContents(file.path, file.original, file.mode); err != nil {
				cause = errors.Join(cause, err)
			} else {
				cause = errors.Join(cause, storage.WriteBytesAtomic(file.path, ".restore-rollback-*", file.rewritten, file.mode))
			}
		}
		return cause
	}
	if err := checkStopped(); err != nil {
		return result, rollback(err)
	}
	for _, file := range files {
		if err := repairSafePath(file.path); err != nil {
			return result, rollback(err)
		}
		if err := rollbackCodexHistoryFile(file); err != nil {
			return result, rollback(err)
		}
		restoredFiles++
	}
	for _, db := range dbs {
		if err := repairDatabaseWriteReady(db.path, checkStopped); err != nil {
			return result, rollback(err)
		}
		if err := rollbackCodexHistoryDatabase(db, manifest.Source); err != nil {
			return result, rollback(err)
		}
		restoredDB++
	}
	result.FileCount = len(files)
	for _, db := range dbs {
		result.ThreadCount += len(db.ids)
	}
	result.Message = "已恢复历史 provider；备份继续保留。"
	return result, nil
}
