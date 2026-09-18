package desktop

import (
	"codexrelay/internal/desktop/clientconfig"
	"errors"
)

func (s *DesktopService) PreviewCodexHistoryRepair(source string) (clientconfig.CodexHistoryRepairResult, error) {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return clientconfig.CodexHistoryRepairResult{}, errors.New("应用尚未初始化")
	}
	return clientconfig.PreviewCodexHistoryRepair(state.Config, source)
}
func (s *DesktopService) ApplyCodexHistoryRepair(source, previewToken string) (clientconfig.CodexHistoryRepairResult, error) {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return clientconfig.CodexHistoryRepairResult{}, errors.New("应用尚未初始化")
	}
	return clientconfig.ApplyCodexHistoryRepair(state.Config, s.runtime.DataDirectory(), source, previewToken)
}
func (s *DesktopService) RestoreCodexHistoryRepair(backupID string) (clientconfig.CodexHistoryRepairResult, error) {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return clientconfig.CodexHistoryRepairResult{}, errors.New("应用尚未初始化")
	}
	return clientconfig.RestoreCodexHistoryRepair(state.Config, s.runtime.DataDirectory(), backupID)
}
