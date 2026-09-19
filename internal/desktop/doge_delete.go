package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"codexrelay/internal/config"
)

// Caller holds clientConfigMu. Keep the established clientConfigMu -> dogeMu
// order and serialize remote deletion with sync, rebinding and base URL changes.
func (s *DesktopService) deleteDogeProfile(profileID string) error {
	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	index := config.FindProfileIndex(state.Config.Profiles, profileID)
	if index < 0 || state.Config.Profiles[index].Source != config.SourceDoge {
		return errors.New("账户密钥已发生变化，请刷新后重试")
	}
	id := state.Config.Profiles[index].RemoteTokenID
	if id <= 0 {
		return errors.New("账户密钥缺少远端令牌 ID，无法删除，请先同步账户")
	}
	if strings.TrimSpace(state.Config.Doge.AccessToken) == "" {
		return errors.New("请先绑定二狗子账户，才能删除账户密钥")
	}
	baseURL := strings.TrimSpace(state.Config.Doge.BaseURL)
	if baseURL == "" {
		baseURL = defaultDogeBaseURL
	}
	client, err := s.newDogeHTTPClient()
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	// A redirect may change DELETE into GET, whose success envelope is not
	// evidence of deletion. Also keep credentials on the configured endpoint.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	// Older persisted records have no service/account provenance. Verify the
	// full key against the current authenticated service before deleting by ID.
	localKey := normalizeDogeAPIKey(state.Config.Profiles[index].APIKey)
	if !isCompleteDogeAPIKey(localKey) {
		return errors.New("账户密钥缺少完整身份信息，无法删除，请先同步账户")
	}
	remoteKey, err := s.fetchDogeTokenKey(context.Background(), client, baseURL, state.Config.Doge.AccessToken, id)
	if err != nil {
		return errors.New("无法核实账户密钥身份，本地记录已保留；请检查账户地址并同步后重试")
	}
	if !isCompleteDogeAPIKey(remoteKey) || remoteKey != localKey {
		return errors.New("账户密钥与当前服务不一致，已取消删除；请同步账户后重新选择")
	}
	response, err := s.dogeRequestEnvelopeWithClient(context.Background(), client, baseURL, state.Config.Doge.AccessToken, http.MethodDelete, fmt.Sprintf("/api/token/%d", id))
	if err != nil {
		if dogeCreateRequestWasRejected(err) {
			return fmt.Errorf("账户密钥删除被拒绝，本地记录已保留: %w", err)
		}
		return fmt.Errorf("账户密钥删除结果未知，本地记录已保留，请先同步账户确认: %w", err)
	}
	// Unlike permissive read endpoints, deletion requires explicit success.
	if !response.Success {
		return errors.New("账户未确认密钥删除成功，本地记录已保留，请先同步账户确认")
	}

	removed := make(map[string]struct{})
	categories := make(map[string]struct{})
	_, err = s.runtime.UpdateConfig(func(cfg *config.AppConfig) error {
		tokens := make([]config.DogeToken, 0, len(cfg.Doge.Tokens))
		for _, token := range cfg.Doge.Tokens {
			if token.ID != id {
				tokens = append(tokens, token)
			}
		}
		cfg.Doge.Tokens = tokens
		cfg.Doge.TokenOrder = mergeDogeTokenOrder(cfg.Doge.TokenOrder, tokens)
		profiles := make([]config.Profile, 0, len(cfg.Profiles))
		for _, profile := range cfg.Profiles {
			if profile.Source != config.SourceDoge || profile.RemoteTokenID != id {
				profiles = append(profiles, profile)
				continue
			}
			removed[profile.ID] = struct{}{}
			if cfg.ActiveProfiles[profile.Category] == profile.ID {
				categories[profile.Category] = struct{}{}
				delete(cfg.ActiveProfiles, profile.Category)
			}
		}
		cfg.Profiles = profiles
		cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, profiles)
		return nil
	})
	if err != nil {
		return fmt.Errorf("账户密钥已删除，但本地保存失败；远端删除无法回退，请同步账户更新本地记录: %w", err)
	}
	s.clearDogeProfileSwitchState(removed, categories)
	return nil
}

func (s *DesktopService) clearDogeProfileSwitchState(removed, categories map[string]struct{}) {
	s.switchMu.Lock()
	for category := range categories {
		delete(s.switchRounds, category)
	}
	for category, context := range s.directorySwitches {
		if context != nil {
			if _, deleted := removed[context.profile.ID]; deleted {
				delete(s.directorySwitches, category)
			}
		}
	}
	for _, notices := range []map[string]*PublicDogeTokenSwitchPrompt{s.directoryRecoveryNotices, s.autoSwitchNotices} {
		for category, notice := range notices {
			if notice != nil {
				if _, deleted := removed[notice.CurrentProfileID]; deleted {
					delete(notices, category)
				}
			}
		}
	}
	for key := range s.switchPrompts {
		for profileID := range removed {
			if strings.HasPrefix(key, profileID+"|") {
				delete(s.switchPrompts, key)
				break
			}
		}
	}
	s.switchMu.Unlock()
}
