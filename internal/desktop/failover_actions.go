package desktop

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
	"codexrelay/internal/relay"
)

// ReorderFailoverProfiles 保存指定类别的统一 Profile 顺序；来源不同的 Profile 可以在同一列表中相邻排列。
// 前端只提交当前视图中的 Profile ID，后端保留未显示项并拒绝跨类别或未知 ID。
func (s *DesktopService) ReorderFailoverProfiles(category string, ids []string) error {
	category = strings.TrimSpace(category)
	if !config.IsCategory(category) {
		return errors.New("API 类别无效")
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		known := make(map[string]struct{})
		for _, profile := range cfg.Profiles {
			if profile.Category == category {
				known[profile.ID] = struct{}{}
			}
		}
		seen := make(map[string]struct{}, len(ids))
		next := make([]string, 0, len(known))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if _, ok := known[id]; !ok {
				return errors.New("令牌切换顺序包含未知或跨类别 Profile")
			}
			if _, ok := seen[id]; ok {
				return errors.New("令牌切换顺序包含重复 Profile")
			}
			seen[id] = struct{}{}
			next = append(next, id)
		}
		for _, id := range config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)[category] {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			next = append(next, id)
		}
		if cfg.FailoverOrder == nil {
			cfg.FailoverOrder = map[string][]string{}
		}
		cfg.FailoverOrder[category] = next
		return nil
	}); err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
}

// DismissDogeTokenSwitch 在当前失败状态持续期间抑制令牌切换提示，并在失败恢复后的五分钟内继续抑制。
// 抑制状态仅保存在内存中，不修改便携配置；失败状态恢复后会自动清理。
func (s *DesktopService) DismissDogeTokenSwitch(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("令牌切换提示已失效")
	}
	s.switchMu.Lock()
	promptState, ok := s.switchPrompts[key]
	autoNotice := false
	recoveryNotice := false
	for category, notice := range s.autoSwitchNotices {
		if notice != nil && notice.Key == key {
			delete(s.autoSwitchNotices, category)
			autoNotice = true
			break
		}
	}
	for category, notice := range s.directoryRecoveryNotices {
		if notice != nil && notice.Key == key {
			delete(s.directoryRecoveryNotices, category)
			recoveryNotice = true
			break
		}
	}
	if ok {
		promptState.dismissed = true
		promptState.suppressedUntil = time.Now().Add(5 * time.Minute)
	}
	s.switchMu.Unlock()
	if !ok && !autoNotice && !recoveryNotice {
		return errors.New("令牌切换提示已失效")
	}
	s.notifyStateChanged()
	return nil
}

// SwitchDogeToken 保留旧绑定入口；实际切换已提升为所有来源共用的 Profile 切换。
func (s *DesktopService) SwitchDogeToken(key string, tokenID int64) error {
	state := s.runtime.State()
	if state == nil || tokenID <= 0 {
		return errors.New("令牌切换参数无效")
	}
	for _, profile := range state.Config.Profiles {
		if profile.Source == config.SourceDoge && profile.RemoteTokenID == tokenID {
			return s.SwitchToken(key, profile.ID)
		}
	}
	return s.SwitchToken(key, dogeFailoverProfileID(tokenID))
}

// SwitchToken 在服务端重新校验提示、类别、顺序和可用状态后启用候选 Profile。
// 前端只提交运行时提示 key 与 Profile ID，不能借此切换到其他类别或不可用 Profile。
func (s *DesktopService) SwitchToken(key, profileID string) error {
	key = strings.TrimSpace(key)
	profileID = strings.TrimSpace(profileID)
	if key == "" || profileID == "" {
		return errors.New("令牌切换参数无效")
	}
	s.failoverMu.Lock()
	var prompt *PublicDogeTokenSwitchPrompt
	for _, candidatePrompt := range s.buildTokenSwitchPrompts() {
		if candidatePrompt != nil && candidatePrompt.Key == key {
			prompt = candidatePrompt
			break
		}
	}
	if prompt == nil {
		s.failoverMu.Unlock()
		return errors.New("令牌切换提示已失效，请重新等待下一次异常")
	}
	found := false
	for _, candidate := range prompt.Candidates {
		if candidate.ProfileID == profileID && candidate.Selectable {
			found = true
			break
		}
	}
	if !found {
		s.failoverMu.Unlock()
		return errors.New("候选 Profile 当前不可用")
	}
	if err := s.switchProfile(prompt.Category, prompt.CurrentProfileID, profileID, tokenSwitchCurrentWasRemoved(s.runtime.State(), prompt)); err != nil {
		s.failoverMu.Unlock()
		return err
	}
	s.clearSwitchPrompt(key)
	s.failoverMu.Unlock()
	s.runtime.ResetProfileHealth(prompt.CurrentProfileID)
	s.notifyStateChanged()
	return nil
}

// tokenSwitchCurrentWasRemoved 只为目录删除产生的有效提示放宽当前 Profile 校验。
// 普通健康错误和目录禁用仍要求 ActiveProfiles 精确匹配，不能借过期提示启用任意候选。
func tokenSwitchCurrentWasRemoved(state *relay.State, prompt *PublicDogeTokenSwitchPrompt) bool {
	return state != nil && prompt != nil && prompt.FailureKind == "directory" &&
		state.Config.ActiveProfiles[prompt.Category] == "" && config.FindProfileIndex(state.Config.Profiles, prompt.CurrentProfileID) < 0
}

func (s *DesktopService) switchProfile(category, currentID, candidateID string, allowRemovedCurrent bool) error {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	remoteID, remoteCandidate := dogeTokenIDFromFailoverProfileID(candidateID)
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	initialIndex := config.FindProfileIndex(state.Config.Profiles, candidateID)
	dogeCandidate := remoteCandidate || initialIndex >= 0 && state.Config.Profiles[initialIndex].Source == config.SourceDoge
	if dogeCandidate {
		// 已导入和待导入的二狗子候选都使用 clientConfigMu -> dogeMu。
		// 取锁后重读目录快照，避免同步期间启用刚变为不可用的令牌。
		s.dogeMu.Lock()
		defer s.dogeMu.Unlock()
		state = s.runtime.State()
	}
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	currentMatches := state.Config.ActiveProfiles[category] == currentID
	currentWasRemoved := allowRemovedCurrent && state.Config.ActiveProfiles[category] == "" && config.FindProfileIndex(state.Config.Profiles, currentID) < 0
	if !currentMatches && !currentWasRemoved {
		return errors.New("当前代理 API 已发生变化，请重新等待下一次异常")
	}
	effectiveConfig := state.Config
	index := config.FindProfileIndex(effectiveConfig.Profiles, candidateID)
	preparedRemote := false
	if index < 0 {
		if !remoteCandidate {
			return errors.New("候选 Profile 不存在或类别不匹配")
		}
		effectiveConfig = config.Clone(state.Config)
		var err error
		candidateID, err = upsertDogeTokenProfile(&effectiveConfig, remoteID, true, "")
		if err != nil {
			return err
		}
		index = config.FindProfileIndex(effectiveConfig.Profiles, candidateID)
		preparedRemote = true
	}
	if index < 0 || effectiveConfig.Profiles[index].Category != category {
		return errors.New("候选 Profile 不存在或类别不匹配")
	}
	candidate := effectiveConfig.Profiles[index]
	if !failoverProfileAvailable(effectiveConfig, candidate) {
		return errors.New("候选 Profile 当前不可用")
	}
	clientEntry := state.Config.ClientConfigs[category]
	var configResult clientconfig.ConfigureResult
	clientConfigRendered := false
	// 自动故障切换只能更新已经明确由 CodexRelay 接管的文件；未接管的
	// 客户端必须等待用户在切换提示中确认，避免后台任务擅自接管外部配置。
	if clientconfig.Supports(category) && !clientEntry.SkipConfigReplacement {
		status, inspectErr := clientconfig.Inspect(state.Config, category)
		if inspectErr != nil {
			return fmt.Errorf("检查客户端配置失败: %w", inspectErr)
		}
		if status.Status == "error" {
			return fmt.Errorf("检查客户端配置失败: %s", status.Error)
		}
		if status.Configured {
			var err error
			configResult, err = clientconfig.ConfigureWithResult(effectiveConfig, category, candidate.ID)
			if err != nil {
				return fmt.Errorf("更新客户端配置失败: %w", err)
			}
			clientConfigRendered = true
		}
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		currentMatches := cfg.ActiveProfiles[category] == currentID
		currentWasRemoved := allowRemovedCurrent && cfg.ActiveProfiles[category] == "" && config.FindProfileIndex(cfg.Profiles, currentID) < 0
		if !currentMatches && !currentWasRemoved {
			return errors.New("当前代理 API 已发生变化，请重新等待下一次异常")
		}
		if preparedRemote {
			committedID, commitErr := upsertDogeTokenProfile(cfg, remoteID, true, candidate.ID)
			if commitErr != nil {
				return commitErr
			}
			if committedID != candidate.ID {
				return errors.New("候选代理 API 已被并发修改，请重新切换")
			}
		}
		committedIndex := config.FindProfileIndex(cfg.Profiles, candidate.ID)
		if committedIndex < 0 || cfg.Profiles[committedIndex].Category != category {
			return errors.New("候选 Profile 不存在或类别不匹配")
		}
		committedCandidate := cfg.Profiles[committedIndex]
		if !failoverProfileAvailable(*cfg, committedCandidate) {
			return errors.New("候选 Profile 当前不可用")
		}
		if clientConfigRendered && !sameClientRenderedProfile(candidate, committedCandidate) {
			return errors.New("候选 Profile 的客户端配置字段已被并发修改，请重新切换")
		}
		cfg.ActiveProfiles[category] = candidate.ID
		return nil
	}); err != nil {
		if configResult.Rollback != nil {
			if rollbackErr := configResult.Rollback(); rollbackErr != nil {
				return fmt.Errorf("切换代理 API 失败: %v；外部配置回退失败: %w", err, rollbackErr)
			}
		}
		return err
	}
	return nil
}
