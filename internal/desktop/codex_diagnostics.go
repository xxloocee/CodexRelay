package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
)

var codexDiagnosticLogMu sync.Mutex

type CodexDiagnostics struct {
	UpstreamOrigin       string                            `json:"upstreamOrigin"`
	SelectionFingerprint string                            `json:"selectionFingerprint"`
	BackupDirectory      string                            `json:"backupDirectory"`
	Config               clientconfig.CodexConfigDiagnosis `json:"config"`
	LogPath              string                            `json:"logPath"`
	RequestObserved      bool                              `json:"requestObserved"`
	LastRequestAt        int64                             `json:"lastRequestAt"`
	Message              string                            `json:"message"`
}

func (s *DesktopService) GetCodexDiagnostics() CodexDiagnostics {
	d := CodexDiagnostics{LogPath: filepath.Join(s.runtime.DataDirectory(), "codex-switch-diagnostics.jsonl")}
	d.BackupDirectory = filepath.Join(s.runtime.DataDirectory(), "client-backups", config.CategoryCodex)
	state := s.runtime.State()
	if state == nil {
		d.Message = "程序尚未初始化"
		return d
	}
	d.Config = clientconfig.DiagnoseCodex(state.Config)
	if active := state.Active[config.CategoryCodex]; active != nil {
		d.UpstreamOrigin = active.Target.Scheme + "://" + active.Target.Host
		identity := sha256.Sum256([]byte(active.Profile.ID))
		d.SelectionFingerprint = hex.EncodeToString(identity[:6])
		d.LastRequestAt = active.LastRequestAt.Load()
		d.RequestObserved = d.LastRequestAt > 0
	}
	switch {
	case d.Config.Mode == "official" && d.Config.Configured:
		d.Message = "官方配置结构已校验；官方登录有效性需在 Codex 中确认"
	case d.Config.Mode == "official":
		d.Message = "当前官方配置缺失或存在冲突，请检查 Provider 与登录方式"
	case !d.Config.Configured:
		d.Message = "当前文件未通过 Relay 配置校验"
	case d.RequestObserved:
		d.Message = "配置已校验，已观察到请求进入当前 Relay 配置（不代表上游成功）"
	default:
		d.Message = "配置已校验，尚未观察到请求进入当前 Relay 配置"
	}
	return d
}

// Fixed event/result codes only; errors and config bodies can contain secrets.
func (s *DesktopService) beginCodexSwitch(category string) func(error) {
	if category != config.CategoryCodex {
		return func(error) {}
	}
	s.writeCodexDiagnostic("switch_started", "pending")
	return func(err error) {
		result := "success"
		if err != nil {
			result = "failed_check_configuration_and_rollback"
		}
		s.writeCodexDiagnostic("switch_finished", result)
	}
}

func (s *DesktopService) writeCodexDiagnostic(event, result string) {
	d := s.GetCodexDiagnostics()
	entry := struct {
		Time      string           `json:"time"`
		Event     string           `json:"event"`
		Result    string           `json:"result"`
		Diagnosis CodexDiagnostics `json:"diagnosis"`
	}{time.Now().UTC().Format(time.RFC3339Nano), event, result, d}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	codexDiagnosticLogMu.Lock()
	defer codexDiagnosticLogMu.Unlock()
	if info, err := os.Stat(d.LogPath); err == nil && info.Size() > 2*1024*1024 {
		// One bounded previous file; diagnostics are not a credentials backup.
		if err := os.Rename(d.LogPath, d.LogPath+".previous"); err != nil {
			return
		}
	}
	file, err := os.OpenFile(d.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(data, '\n'))
}
