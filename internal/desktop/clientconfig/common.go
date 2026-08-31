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
	Existed        bool   `json:"existed"`
	Created        bool   `json:"created"`
	Mode           uint32 `json:"mode,omitempty"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
}

// ConfigureResult contains the files changed by ConfigureWithResult. Rollback
// restores the exact bytes and permissions captured before the write.
// Configure keeps the historical error-only API and discards this value.
type ConfigureResult struct {
	Files    []ConfigFileResult `json:"files"`
	Rollback func() error       `json:"-"`
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
func applyConfigChanges(changes []ConfigFileChange) (ConfigureResult, error) {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	return applyConfigTransaction(paths, func(map[string]configFileSnapshot) ([]ConfigFileChange, error) {
		return changes, nil
	})
}

// applyConfigTransaction reads each source exactly once, renders from that
// immutable snapshot, verifies the source did not change, then commits. A
// rollback refuses to overwrite a file that another process changed later.
func applyConfigTransaction(paths []string, render func(map[string]configFileSnapshot) ([]ConfigFileChange, error)) (ConfigureResult, error) {
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
	for _, change := range uniqueChanges {
		snapshot := byPath[change.Path]
		backup := ""
		if snapshot.existed {
			backup, err = backupClientData(snapshot.path, snapshot.data)
			if err != nil {
				return result, err
			}
		}
		result.Files = append(result.Files, ConfigFileResult{
			Path: snapshot.path, BackupPath: backup, Existed: snapshot.existed, Created: !snapshot.existed,
			Mode: uint32(snapshot.mode.Perm()), ExpectedSHA256: sha256Hex(change.Data),
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
	return result, nil
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
	if !snapshot.existed || !bytes.Equal(data, snapshot.data) || info.Mode().Perm() != snapshot.mode.Perm() {
		return fmt.Errorf("%s 在配置期间被其他进程修改", filepath.Base(snapshot.path))
	}
	return nil
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
	if mode != 0 && info.Mode().Perm() != mode.Perm() {
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

// backupClientFile 为外部文件创建带时间戳的 .CodexRelay 备份，不存在的文件不生成空备份。
func backupClientFile(path string) error {
	_, err := backupClientFilePath(path)
	return err
}

func backupClientFilePath(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取备份源文件: %w", err)
	}
	return backupClientData(path, data)
}

func backupClientData(path string, data []byte) (string, error) {
	stamp := time.Now().Format("20060102-150405")
	backup := fmt.Sprintf("%s.%s.CodexRelay", path, stamp)
	for index := 2; pathExists(backup); index++ {
		backup = fmt.Sprintf("%s.%s-%d.CodexRelay", path, stamp, index)
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
