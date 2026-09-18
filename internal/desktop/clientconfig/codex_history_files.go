package clientconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"codexrelay/internal/storage"
)

// Plans retain hashes and paths, never the accumulated conversation bodies.
// At most one rollout and its rewritten counterpart are loaded at a time.
type allRepairDiskFile struct {
	path, backup, before, after string
	mode                        os.FileMode
}

func allRepairFileFingerprints(directory string, files []allRepairDiskFile) []repairFile {
	result := make([]repairFile, 0, len(files))
	for _, file := range files {
		relative, _ := filepath.Rel(directory, file.path)
		result = append(result, repairFile{relative, file.before, file.after, uint32(file.mode)})
	}
	return result
}

func historyFileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func historyFileMatches(file allRepairDiskFile, expected string) error {
	if err := repairSafePath(file.path); err != nil {
		return err
	}
	hash, err := historyFileHash(file.path)
	if err != nil {
		return err
	}
	info, err := os.Stat(file.path)
	if err != nil {
		return err
	}
	if hash != expected || !clientFileModesEqual(info.Mode(), file.mode) {
		return fmt.Errorf("会话 %s 在预览或修复后被修改，拒绝覆盖", file.path)
	}
	return nil
}

func loadRepairBackup(file allRepairDiskFile, target string) ([]byte, []byte, error) {
	if err := repairSafePath(file.backup); err != nil {
		return nil, nil, err
	}
	before, err := os.ReadFile(file.backup)
	if err != nil {
		return nil, nil, err
	}
	after, changed, err := rewriteAllRepairJSONL(before, target)
	if err != nil {
		return nil, nil, err
	}
	if !changed || sha256Hex(before) != file.before || sha256Hex(after) != file.after {
		return nil, nil, errors.New("历史备份完整性校验失败")
	}
	return before, after, nil
}

func writeRepairDiskFile(file allRepairDiskFile, target string, restore bool) error {
	before, after, err := loadRepairBackup(file, target)
	if err != nil {
		return err
	}
	expected, desired := file.before, after
	if restore {
		expected, desired = file.after, before
	}
	if err := historyFileMatches(file, expected); err != nil {
		return err
	}
	return storage.WriteBytesAtomic(file.path, ".history-repair-*", desired, file.mode)
}

func validateRepairDiskFiles(files []allRepairDiskFile, target string, restore bool) ([]allRepairDiskFile, error) {
	var pending []allRepairDiskFile
	for _, file := range files {
		if _, _, err := loadRepairBackup(file, target); err != nil {
			return nil, err
		}
		if restore && historyFileMatches(file, file.before) == nil {
			continue // Interrupted before this file, or an earlier restore already finished it.
		}
		expected := file.before
		if restore {
			expected = file.after
		}
		if err := historyFileMatches(file, expected); err != nil {
			return nil, err
		}
		pending = append(pending, file)
	}
	return pending, nil
}
