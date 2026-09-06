/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 外部客户端配置适配器共用的备份、写入和模型选择逻辑
 * @File          : 客户端配置公共辅助
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package clientconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"codexrelay/internal/config"
	"codexrelay/internal/storage"
)

// ConfigFileResult describes one external file touched by a configuration
// transaction. BackupPath is empty when the file did not exist before the
// transaction (new files are removed by Rollback).
type ConfigFileResult struct {
	Path           string `json:"path"`
	BackupPath     string `json:"backupPath,omitempty"`
	BackupSHA256   string `json:"backupSha256,omitempty"`
	Existed        bool   `json:"existed"`
	Created        bool   `json:"created"`
	Mode           uint32 `json:"mode,omitempty"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
}

// ConfigureResult contains the files changed by ConfigureWithResult. Rollback
// restores the exact bytes and permissions captured before the write.
type ConfigureResult struct {
	Files                 []ConfigFileResult `json:"files"`
	Rollback              func() error       `json:"-"`
	OfficialSnapshot      bool               `json:"-"`
	ResetOfficialSnapshot bool               `json:"-"`
}

type configFileSnapshot struct {
	path    string
	data    []byte
	mode    os.FileMode
	existed bool
}

// applyConfigChanges backs up every existing input, then atomically commits
// all files. A failed write restores all files already written and removes
// files created by this transaction.
func applyConfigChanges(changes []ConfigFileChange, backupDirectory ...string) (ConfigureResult, error) {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	return applyConfigTransaction(paths, func(map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		return changes, nil
	}, backupDirectory...)
}

// applyConfigTransaction reads each source exactly once, renders from that
// immutable snapshot, verifies the source did not change, then commits. A
// rollback refuses to overwrite a file that another process changed later.
func applyConfigTransaction(paths []string, render func(map[string]configFileSnapshot) ([]ConfigFileChange, error), backupDirectory ...string) (ConfigureResult, error) {
	return applyConfigTransactionWithBackupPolicy(paths, render, nil, backupDirectory...)
}

// applyConfigTransactionWithBackupPolicy allows callers that already have an
// official snapshot to skip creating another unreferenced copy. A nil policy
// backs up every existing source file.
func applyConfigTransactionWithBackupPolicy(paths []string, render func(map[string]configFileSnapshot) ([]ConfigFileChange, error), shouldBackup func(string) bool, backupDirectory ...string) (ConfigureResult, error) {
	result := ConfigureResult{}
	snapshots := make([]configFileSnapshot, 0, len(paths))
	byPath := make(map[string]configFileSnapshot, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if _, exists := byPath[path]; exists {
			continue
		}
		change := ConfigFileChange{Path: path}
		snapshot := configFileSnapshot{path: change.Path, mode: 0o600}
		info, err := os.Stat(change.Path)
		if errors.Is(err, os.ErrNotExist) {
			// 不存在的目标是合法的新配置文件；提交失败时由 Rollback 删除。
			err = nil
		} else if err != nil {
			return result, fmt.Errorf("读取 %s: %w", filepath.Base(change.Path), err)
		} else {
			if info.IsDir() {
				return result, fmt.Errorf("配置目标 %s 是目录", filepath.Base(change.Path))
			}
			snapshot.existed = true
			snapshot.mode = info.Mode().Perm()
			snapshot.data, err = os.ReadFile(change.Path)
			if err != nil {
				return result, fmt.Errorf("读取 %s: %w", filepath.Base(change.Path), err)
			}
		}
		snapshots = append(snapshots, snapshot)
		byPath[path] = snapshot
	}
	changes, err := render(byPath)
	if err != nil {
		return result, err
	}
	uniqueChanges := make([]ConfigFileChange, 0, len(changes))
	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		if seen[change.Path] {
			continue
		}
		if _, ok := byPath[change.Path]; !ok {
			return result, fmt.Errorf("配置事务包含未预读的目标 %s", filepath.Base(change.Path))
		}
		seen[change.Path] = true
		uniqueChanges = append(uniqueChanges, change)
	}

	// Back up before changing any file. This also means a parse/generation
	// failure cannot leave behind a misleading backup.
	result.Files = make([]ConfigFileResult, 0, len(uniqueChanges))
	createdBackups := make([]string, 0, len(uniqueChanges))
	cleanupBackups := func() error {
		var cleanupErr error
		for _, path := range createdBackups {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, removeErr)
			}
		}
		return cleanupErr
	}
	for _, change := range uniqueChanges {
		snapshot := byPath[change.Path]
		backup := ""
		if snapshot.existed && (shouldBackup == nil || shouldBackup(snapshot.path)) {
			backup, err = backupClientData(snapshot.path, snapshot.data, backupDirectory...)
			if err != nil {
				_ = cleanupBackups()
				return result, err
			}
			createdBackups = append(createdBackups, backup)
		}
		backupSHA256 := ""
		if snapshot.existed {
			backupSHA256 = sha256Hex(snapshot.data)
		}
		result.Files = append(result.Files, ConfigFileResult{
			Path: snapshot.path, BackupPath: backup, Existed: snapshot.existed, Created: !snapshot.existed,
			Mode: uint32(snapshot.mode.Perm()), ExpectedSHA256: sha256Hex(change.Data), BackupSHA256: backupSHA256,
		})
	}

	type writtenConfigFile struct {
		data []byte
		mode os.FileMode
	}
	written := make(map[string]writtenConfigFile, len(uniqueChanges))
	rollback := func() error {
		var rollbackErr error
		for index := len(snapshots) - 1; index >= 0; index-- {
			snapshot := snapshots[index]
			expected, ok := written[snapshot.path]
			if !ok {
				continue
			}
			current, readErr := os.ReadFile(snapshot.path)
			info, statErr := os.Stat(snapshot.path)
			if readErr != nil || statErr != nil || !bytes.Equal(current, expected.data) || info.Mode().Perm() != expected.mode.Perm() {
				if rollbackErr == nil {
					rollbackErr = fmt.Errorf("恢复 %s: 文件已被其他进程修改", filepath.Base(snapshot.path))
				}
				continue
			}
			var err error
			if snapshot.existed {
				err = writeClientFileRollback(snapshot.path, snapshot.data, snapshot.mode)
			} else {
				err = os.Remove(snapshot.path)
				if errors.Is(err, os.ErrNotExist) {
					err = nil
				}
			}
			if err != nil && rollbackErr == nil {
				rollbackErr = fmt.Errorf("恢复 %s: %w", filepath.Base(snapshot.path), err)
			}
		}
		// If a concurrent writer changed a committed file, keep the newly
		// captured backups. They may be the only safe recovery copy once this
		// transaction declines to overwrite that external change.
		if rollbackErr == nil {
			if err := cleanupBackups(); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		return rollbackErr
	}
	result.Rollback = rollback

	for _, change := range uniqueChanges {
		snapshot := byPath[change.Path]
		if err := ensureSnapshotUnchanged(snapshot); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return result, &ConfigureTransactionError{Err: err, Result: result, RollbackErr: rollbackErr}
			}
			return result, &ConfigureTransactionError{Err: err, Result: result}
		}
		if err := writeClientFileWithMode(change.Path, change.Data, snapshot.mode); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return result, &ConfigureTransactionError{Err: err, Result: result, RollbackErr: rollbackErr}
			}
			return result, &ConfigureTransactionError{Err: err, Result: result}
		}
		written[change.Path] = writtenConfigFile{data: append([]byte(nil), change.Data...), mode: snapshot.mode}
	}
	// Verify the complete batch after the last write. Without this pass an
	// external client can rewrite an earlier file while a later file is being
	// committed, leaving a partially managed configuration reported as success.
	for _, change := range uniqueChanges {
		expected := written[change.Path]
		if err := ensureWrittenConfigUnchanged(change.Path, expected.data, expected.mode); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return result, &ConfigureTransactionError{Err: err, Result: result, RollbackErr: rollbackErr}
			}
			return result, &ConfigureTransactionError{Err: err, Result: result}
		}
	}
	return result, nil
}

func ensureWrittenConfigUnchanged(path string, expected []byte, mode os.FileMode) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("提交后检查 %s: %w", filepath.Base(path), err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("提交后检查 %s: %w", filepath.Base(path), err)
	}
	if !bytes.Equal(data, expected) || !clientFileModesEqual(info.Mode(), mode) {
		return fmt.Errorf("%s 在配置提交后被其他进程修改", filepath.Base(path))
	}
	return nil
}

func ensureSnapshotUnchanged(snapshot configFileSnapshot) error {
	data, err := os.ReadFile(snapshot.path)
	if errors.Is(err, os.ErrNotExist) && !snapshot.existed {
		return nil
	}
	if err != nil {
		return fmt.Errorf("提交前检查 %s: %w", filepath.Base(snapshot.path), err)
	}
	info, err := os.Stat(snapshot.path)
	if err != nil {
		return fmt.Errorf("提交前检查 %s: %w", filepath.Base(snapshot.path), err)
	}
	if !snapshot.existed || !bytes.Equal(data, snapshot.data) || !clientFileModesEqual(info.Mode(), snapshot.mode) {
		return fmt.Errorf("%s 在配置期间被其他进程修改", filepath.Base(snapshot.path))
	}
	return nil
}

// Windows does not preserve Unix permission bits through MoveFileEx and may
// report a different Perm value after an atomic replacement. Contents remain
// the reliable concurrency marker there; Unix keeps the stricter mode check.
func clientFileModesEqual(actual, expected os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return actual.Perm() == expected.Perm()
}

// ConfigFileChange is one generated external configuration file.
type ConfigFileChange struct {
	Path string
	Data []byte
}

// ConfigureTransactionError reports a failed atomic client configuration and
// includes the rollback details for logging or user-facing recovery actions.
type ConfigureTransactionError struct {
	Err         error
	Result      ConfigureResult
	RollbackErr error
}

// RestoreConfigFile restores one file from a ConfigFileResult backup. It is
// usable after process restart when only the persisted backup path remains.
// New files are removed because they have no pre-transaction contents.
func RestoreConfigFile(file ConfigFileResult) error {
	if strings.TrimSpace(file.ExpectedSHA256) == "" {
		return errors.New("缺少配置回退校验指纹，拒绝覆盖当前文件")
	}
	if err := verifyConfigFileResult(file); err != nil {
		return err
	}
	if !file.Existed {
		err := os.Remove(file.Path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(file.BackupPath) == "" {
		return errors.New("缺少原配置备份路径")
	}
	data, err := os.ReadFile(file.BackupPath)
	if err != nil {
		return fmt.Errorf("读取配置备份失败: %w", err)
	}
	mode := os.FileMode(file.Mode)
	if mode == 0 {
		mode = 0o600
	}
	if file.Mode == 0 {
		if info, statErr := os.Stat(file.Path); statErr == nil {
			mode = info.Mode().Perm()
		}
	}
	return writeClientFileRollback(file.Path, data, mode)
}

func verifyConfigFileResult(file ConfigFileResult) error {
	data, err := os.ReadFile(file.Path)
	info, statErr := os.Stat(file.Path)
	if errors.Is(err, os.ErrNotExist) && errors.Is(statErr, os.ErrNotExist) {
		if !file.Existed {
			return nil
		}
		return fmt.Errorf("恢复 %s 失败: 当前文件不存在", filepath.Base(file.Path))
	}
	if err != nil {
		return fmt.Errorf("检查 %s 失败: %w", filepath.Base(file.Path), err)
	}
	if statErr != nil {
		return fmt.Errorf("检查 %s 失败: %w", filepath.Base(file.Path), statErr)
	}
	if sha256Hex(data) != strings.TrimSpace(file.ExpectedSHA256) {
		return fmt.Errorf("恢复 %s 失败: 文件已被其他进程修改", filepath.Base(file.Path))
	}
	mode := os.FileMode(file.Mode)
	if mode != 0 && !clientFileModesEqual(info.Mode(), mode) {
		return fmt.Errorf("恢复 %s 失败: 文件权限已被其他进程修改", filepath.Base(file.Path))
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// RestoreConfigFiles restores a batch in reverse order, which is useful for a
// multi-file adapter when its transaction metadata was persisted by a caller.
func RestoreConfigFiles(files []ConfigFileResult) error {
	var firstErr error
	for index := len(files) - 1; index >= 0; index-- {
		if err := RestoreConfigFile(files[index]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// RestoreOfficialConfig restores the exact files captured before CodexRelay
// first took over a client. The caller must clear the persisted snapshot only
// after this function succeeds.
func RestoreOfficialConfig(files []config.ClientConfigBackup) error {
	_, err := RestoreOfficialConfigWithRollback(files)
	return err
}

func RestoreOfficialConfigWithRollback(files []config.ClientConfigBackup) (func() error, error) {
	if len(files) == 0 {
		return nil, errors.New("没有可恢复的官方客户端配置")
	}
	converted := make([]ConfigFileResult, 0, len(files))
	for _, file := range files {
		converted = append(converted, ConfigFileResult{
			Path: file.Path, BackupPath: file.BackupPath, Existed: file.Existed,
			Mode: file.Mode, ExpectedSHA256: file.ExpectedSHA256, BackupSHA256: file.BackupSHA256,
		})
	}
	return restoreConfigFilesTransaction(converted)
}

type restoreTarget struct {
	file           ConfigFileResult
	desired        []byte
	current        []byte
	currentMode    os.FileMode
	currentExisted bool
}

// restoreConfigFilesTransaction verifies every target before changing any
// file, then restores the complete batch. A failed write restores the files
// already changed from their in-memory snapshots.
func restoreConfigFilesTransaction(files []ConfigFileResult) (func() error, error) {
	targets := make([]restoreTarget, 0, len(files))
	for _, file := range files {
		if err := verifyConfigFileResult(file); err != nil {
			return nil, err
		}
		target := restoreTarget{file: file}
		if file.Existed {
			data, err := os.ReadFile(file.BackupPath)
			if err != nil {
				return nil, fmt.Errorf("读取 %s 官方备份失败: %w", filepath.Base(file.Path), err)
			}
			if file.BackupSHA256 == "" || sha256Hex(data) != file.BackupSHA256 {
				return nil, fmt.Errorf("%s 官方备份校验失败", filepath.Base(file.Path))
			}
			target.desired = data
		}
		data, err := os.ReadFile(file.Path)
		if errors.Is(err, os.ErrNotExist) {
			data = nil
		} else if err != nil {
			return nil, fmt.Errorf("读取 %s 当前内容失败: %w", filepath.Base(file.Path), err)
		} else {
			target.current = data
			target.currentExisted = true
			if info, statErr := os.Stat(file.Path); statErr == nil {
				target.currentMode = info.Mode().Perm()
			} else {
				return nil, fmt.Errorf("读取 %s 当前权限失败: %w", filepath.Base(file.Path), statErr)
			}
		}
		targets = append(targets, target)
	}
	applied := make([]restoreTarget, 0, len(targets))
	rollback := func() error {
		var result error
		for index := len(applied) - 1; index >= 0; index-- {
			result = errors.Join(result, restoreCurrentConfigFile(applied[index]))
		}
		return result
	}
	for _, target := range targets {
		if err := ensureRestoreTargetUnchanged(target); err != nil {
			return nil, errors.Join(fmt.Errorf("恢复 %s 失败: %w", filepath.Base(target.file.Path), err), rollback())
		}
		var err error
		if target.file.Existed {
			mode := os.FileMode(target.file.Mode)
			if mode == 0 {
				mode = 0o600
			}
			err = writeClientFileRollback(target.file.Path, target.desired, mode)
		} else {
			err = os.Remove(target.file.Path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			return nil, errors.Join(fmt.Errorf("恢复 %s 失败: %w", filepath.Base(target.file.Path), err), rollback())
		}
		applied = append(applied, target)
	}
	// Verify the complete restore after the last write. A client may rewrite a
	// file while another restore target is being committed; never report
	// success or consume the official snapshot in that case.
	for _, target := range applied {
		if err := ensureRestoredConfigUnchanged(target); err != nil {
			return nil, errors.Join(fmt.Errorf("恢复 %s 失败: %w", filepath.Base(target.file.Path), err), rollback())
		}
	}
	return rollback, nil
}

func ensureRestoredConfigUnchanged(target restoreTarget) error {
	data, err := os.ReadFile(target.file.Path)
	info, statErr := os.Stat(target.file.Path)
	if !target.file.Existed {
		if errors.Is(err, os.ErrNotExist) && errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		return errors.New("当前文件已被其他进程修改")
	}
	if err != nil || statErr != nil || !bytes.Equal(data, target.desired) {
		return errors.New("当前文件已被其他进程修改")
	}
	mode := os.FileMode(target.file.Mode)
	if mode != 0 && info.Mode().Perm() != mode.Perm() {
		return errors.New("当前文件权限已被其他进程修改")
	}
	return nil
}

func ensureRestoreTargetUnchanged(target restoreTarget) error {
	data, err := os.ReadFile(target.file.Path)
	info, statErr := os.Stat(target.file.Path)
	if !target.currentExisted {
		if errors.Is(err, os.ErrNotExist) && errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		return errors.New("当前文件已被其他进程修改")
	}
	if err != nil || statErr != nil || !bytes.Equal(data, target.current) || !clientFileModesEqual(info.Mode(), target.currentMode) {
		return errors.New("当前文件已被其他进程修改")
	}
	return nil
}

func restoreCurrentConfigFile(target restoreTarget) error {
	if !target.file.Existed {
		if _, err := os.Stat(target.file.Path); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return errors.New("当前文件已被其他进程修改")
		}
		if !target.currentExisted {
			return nil
		}
		return writeClientFileRollback(target.file.Path, target.current, target.currentMode)
	}
	current, err := os.ReadFile(target.file.Path)
	info, statErr := os.Stat(target.file.Path)
	if !target.currentExisted {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return errors.New("当前文件已被其他进程修改")
	}
	desiredMode := os.FileMode(target.file.Mode)
	if desiredMode == 0 {
		desiredMode = 0o600
	}
	if err != nil || statErr != nil || !bytes.Equal(current, target.desired) || info.Mode().Perm() != desiredMode.Perm() {
		return errors.New("当前文件已被其他进程修改")
	}
	return writeClientFileRollback(target.file.Path, target.current, target.currentMode)
}

func (e *ConfigureTransactionError) Error() string {
	if e.RollbackErr != nil {
		return fmt.Sprintf("配置提交失败: %v；回退失败: %v", e.Err, e.RollbackErr)
	}
	return fmt.Sprintf("配置提交失败: %v", e.Err)
}

func (e *ConfigureTransactionError) Unwrap() error { return e.Err }

// activeProfileForClient 从本地配置快照选择指定分类的模型目录，不触发网络请求。
func activeProfileForClient(cfg config.AppConfig, category, profileID string) *config.Profile {
	if profileID == "" {
		profileID = cfg.ActiveProfiles[category]
	}
	index := config.FindProfileIndex(cfg.Profiles, profileID)
	if index < 0 || cfg.Profiles[index].Category != category {
		return nil
	}
	profile := config.CloneProfile(cfg.Profiles[index])
	return &profile
}

// ClientBackupDirectory returns the private Relay-owned directory for one
// client's original configuration snapshots.
func ClientBackupDirectory(dataDirectory, category string) string {
	return filepath.Join(filepath.Clean(dataDirectory), "client-backups", category)
}

func backupClientData(path string, data []byte, backupDirectory ...string) (string, error) {
	stamp := time.Now().Format("20060102-150405")
	if len(backupDirectory) == 0 || strings.TrimSpace(backupDirectory[0]) == "" {
		// Preserve the historical helper API: callers that do not provide Relay's
		// data directory keep backups beside the source file.
		backup := fmt.Sprintf("%s.%s.CodexRelay", path, stamp)
		for index := 2; pathExists(backup); index++ {
			backup = fmt.Sprintf("%s.%s-%d.CodexRelay", path, stamp, index)
		}
		if err := storage.WriteBytesAtomic(backup, ".codexrelay-backup-*.tmp", data, 0o600); err != nil {
			return "", fmt.Errorf("创建 %s 备份: %w", filepath.Base(path), err)
		}
		return backup, nil
	}
	destination := filepath.Clean(backupDirectory[0])
	if !filepath.IsAbs(destination) {
		return "", errors.New("客户端备份目录必须是绝对路径")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return "", fmt.Errorf("创建客户端备份目录: %w", err)
	}
	backup := filepath.Join(destination, fmt.Sprintf("%s.%s.CodexRelay", filepath.Base(path), stamp))
	for index := 2; pathExists(backup); index++ {
		backup = filepath.Join(destination, fmt.Sprintf("%s.%s-%d.CodexRelay", filepath.Base(path), stamp, index))
	}
	if err := storage.WriteBytesAtomic(backup, ".codexrelay-backup-*.tmp", data, 0o600); err != nil {
		return "", fmt.Errorf("创建 %s 备份: %w", filepath.Base(path), err)
	}
	return backup, nil
}

// writeClientFile 通过共享的原子 JSON 存储写入外部配置，失败时保留原文件。
func writeClientFile(path string, data []byte) error {
	return writeClientFileWithMode(path, data, 0o600)
}

func writeClientFileWithMode(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	return storage.WriteBytesAtomic(path, ".codexrelay-config-*.tmp", data, mode)
}

func writeClientFileRollback(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	return storage.WriteBytesAtomic(path, ".codexrelay-rollback-*.tmp", data, mode)
}

func containsModel(models []config.ModelEntry, id string) bool {
	for _, model := range models {
		if model.ID == id {
			return true
		}
	}
	return false
}

func selectedModelID(models []config.ModelEntry, defaultModel string) string {
	if defaultModel != "" && containsModel(models, defaultModel) {
		return defaultModel
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}

func validateExternalValue(name, value string) error {
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s 不能包含控制字符", name)
		}
	}
	return nil
}
