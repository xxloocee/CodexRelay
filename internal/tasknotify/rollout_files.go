package tasknotify

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codexrelay/internal/storage"
)

func (m *Manager) findRolloutsLocked() ([]string, error) {
	root, err := m.sessionsDirectory()
	if err != nil {
		return nil, err
	}
	return findRollouts(root)
}

func defaultSessionsDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

func findRollouts(root string) ([]string, error) {
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	} else if err != nil {
		return nil, err
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "rollout-") && strings.HasSuffix(entry.Name(), ".jsonl") {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func eventKey(threadID, turnID string) string {
	digest := sha256.Sum256([]byte("codexrelay/task-notification/v1\x00" + threadID + "\x00" + turnID))
	return hex.EncodeToString(digest[:])
}

func externalEventKey(eventType, identity string) string {
	digest := sha256.Sum256([]byte("codexrelay/notification-event/v1\x00" + eventType + "\x00" + identity))
	return hex.EncodeToString(digest[:])
}

func rolloutEventType(lifecycle string) string {
	switch lifecycle {
	case "task_complete":
		return EventTaskCompleted
	case "turn_aborted":
		return EventTaskAborted
	default:
		return ""
	}
}

func knownEventType(eventType string) bool {
	switch eventType {
	case EventTaskCompleted, EventTaskAborted, EventTokenRequestFailed, EventTokenAutoSwitched, EventTokenAutoSwitchFailed, EventAccountBalanceLow, EventSubscriptionBalanceLow:
		return true
	default:
		return false
	}
}

func eventEnabled(events EventSettings, eventType string) bool {
	switch eventType {
	case EventTaskCompleted:
		return events.TaskCompleted
	case EventTaskAborted:
		return events.TaskAborted
	case EventTokenRequestFailed:
		return events.TokenRequestFailed
	case EventTokenAutoSwitched:
		return events.TokenAutoSwitched
	case EventTokenAutoSwitchFailed:
		return events.TokenAutoSwitchFailed
	case EventAccountBalanceLow:
		return events.AccountBalanceLow
	case EventSubscriptionBalanceLow:
		return events.SubscriptionBalanceLow
	default:
		return false
	}
}

func readRecord(path string) (record, error) {
	var entry record
	exists, err := storage.ReadJSON(path, &entry)
	if err != nil || !exists || entry.Schema != 1 || entry.Key == "" || !knownEventType(entry.EventType) {
		return record{}, errors.New("记录格式无效")
	}
	if entry.EventID == "" && (entry.TurnID == "" || entry.RolloutPath == "") {
		return record{}, errors.New("记录格式无效")
	}
	return entry, nil
}

func jsonFiles(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}
func countJSONFiles(directory string) int { return len(jsonFiles(directory)) }
