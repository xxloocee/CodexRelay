package desktop

import (
	"fmt"
	"strings"
	"time"

	"codexrelay/internal/config"
)

// dogeDirectorySwitchContexts 返回按类别保存的目录失效快照。
// 快照只保留同步前当前令牌和顺序锚点；候选始终按调用时配置重算，且这些状态都不写入配置文件。
func (s *DesktopService) dogeDirectorySwitchContexts() map[string]*tokenSwitchContext {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	result := make(map[string]*tokenSwitchContext, len(s.directorySwitches))
	for category, source := range s.directorySwitches {
		if source == nil {
			continue
		}
		context := *source
		context.tokens = append([]config.DogeToken(nil), source.tokens...)
		context.groups = append([]string(nil), source.groups...)
		context.candidateProfiles = append([]config.Profile(nil), source.candidateProfiles...)
		context.failoverOrder = append([]string(nil), source.failoverOrder...)
		context.profilesByID = make(map[int64]config.Profile, len(source.profilesByID))
		for id, profile := range source.profilesByID {
			context.profilesByID[id] = profile
		}
		result[category] = &context
	}
	return result
}

// cloneDogeDirectorySwitchContexts 复制目录失效快照中会跨同步保留的字段。
// 恢复判断必须使用上一次同步看到的令牌和分组状态，不能在配置写入后再读取旧快照。
func cloneDogeDirectorySwitchContexts(source map[string]*tokenSwitchContext) map[string]*tokenSwitchContext {
	result := make(map[string]*tokenSwitchContext, len(source))
	for category, item := range source {
		if item == nil {
			continue
		}
		clone := *item
		clone.tokens = append([]config.DogeToken(nil), item.tokens...)
		clone.groups = append([]string(nil), item.groups...)
		clone.failoverOrder = append([]string(nil), item.failoverOrder...)
		clone.candidateProfiles = append([]config.Profile(nil), item.candidateProfiles...)
		clone.profilesByID = make(map[int64]config.Profile, len(item.profilesByID))
		for id, profile := range item.profilesByID {
			clone.profilesByID[id] = profile
		}
		result[category] = &clone
	}
	return result
}

// recoveredDogeTokens 返回同一类别中从上一次不可用状态恢复为可用的令牌。
// 旧快照只来自目录失效上下文，且恢复必须同时满足 status、分组权限和完整密钥条件。
func recoveredDogeTokens(previous *tokenSwitchContext, cfg config.AppConfig) []config.DogeToken {
	if previous == nil {
		return nil
	}
	currentByID := make(map[int64]config.DogeToken, len(cfg.Doge.Tokens))
	for _, token := range cfg.Doge.Tokens {
		currentByID[token.ID] = token
	}
	result := make([]config.DogeToken, 0)
	seen := make(map[int64]struct{})
	for _, oldToken := range previous.tokens {
		if oldToken.ID <= 0 || dogeTokenAvailable(oldToken, previous.groups) {
			continue
		}
		profile, belongs := previous.profilesByID[oldToken.ID]
		if !belongs || profile.Category != previous.profile.Category {
			continue
		}
		current, exists := currentByID[oldToken.ID]
		if !exists || !dogeTokenSwitchable(current, cfg.Doge.Groups) {
			continue
		}
		if _, ok := seen[current.ID]; ok {
			continue
		}
		seen[current.ID] = struct{}{}
		result = append(result, current)
	}
	return result
}

// buildDogeDirectoryRecoveryNotice 生成“令牌已恢复”提示；候选使用当前类别的全部可用 Profile，
// 但排除当前仍在使用的 Profile，避免用户把切换操作提交为无变化。
func buildDogeDirectoryRecoveryNotice(cfg config.AppConfig, previous *tokenSwitchContext, recovered []config.DogeToken, candidates []config.Profile) *PublicDogeTokenSwitchPrompt {
	if previous == nil {
		return nil
	}
	// 令牌失效期间自动切换可能已经改写 ActiveProfiles；恢复提示必须以此刻实际活动项作为
	// 当前 Profile，恢复的令牌才会进入候选列表，SwitchToken 的并发状态校验也才能成立。
	currentProfile := previous.profile
	if activeID := strings.TrimSpace(cfg.ActiveProfiles[previous.profile.Category]); activeID != "" {
		if index := config.FindProfileIndex(cfg.Profiles, activeID); index >= 0 && cfg.Profiles[index].Category == previous.profile.Category {
			currentProfile = cfg.Profiles[index]
		}
	}
	current := dogeTokenForProfile(cfg, currentProfile)
	currentName := strings.TrimSpace(currentProfile.Name)
	if currentName == "" {
		currentName = current.Name
	}
	currentName = formatNonHomeProfileName(currentName, currentProfile.Source, dogeTokenDisplayGroup(current), current.GroupRatio)
	if currentName == "" {
		currentName = "当前令牌"
	}
	names := make([]string, 0, len(recovered))
	for _, token := range recovered {
		name := strings.TrimSpace(token.Name)
		if profile, ok := previous.profilesByID[token.ID]; ok && strings.TrimSpace(profile.Name) != "" {
			name = strings.TrimSpace(profile.Name)
		}
		name = formatDogeProfileName(name, dogeTokenDisplayGroup(token), token.GroupRatio)
		if name != "" {
			names = append(names, "“"+name+"”")
		}
	}
	if len(names) == 0 {
		return nil
	}
	message := fmt.Sprintf("Codex类别（%s）下%s令牌已恢复可用。", categoryDisplayName(previous.profile.Category), strings.Join(names, "、"))
	return &PublicDogeTokenSwitchPrompt{
		Key: previous.key + "|recovered", Category: previous.profile.Category, Mode: "manual", FailureKind: "directory_recovered",
		CurrentTokenID: currentProfile.RemoteTokenID, CurrentProfileID: currentProfile.ID, CurrentName: currentName,
		CurrentGroup: dogeTokenDisplayGroup(current), CurrentRatio: current.GroupRatio, Message: message,
		Candidates: publicDogeTokenSwitchCandidates(cfg, candidates, currentProfile.ID),
	}
}

func categoryDisplayName(category string) string {
	switch category {
	case config.CategoryCodex:
		return "Codex"
	case config.CategoryClaude:
		return "Claude"
	case config.CategoryGemini:
		return "Gemini"
	case config.CategoryGrok:
		return "Grok"
	case config.CategoryOpenCode:
		return "OpenCode"
	case config.CategoryOpenClaw:
		return "OpenClaw"
	case config.CategoryHermes:
		return "Hermes"
	case config.CategoryImage:
		return "生图"
	case config.CategoryOther:
		return "其他"
	default:
		return category
	}
}

// setDogeDirectorySwitchContexts 替换自动同步检测到的各类别目录失效提示。
// 提示 key 在同一活动令牌持续失效期间保持稳定，用户取消后沿用现有五分钟及持续期间抑制规则。
func (s *DesktopService) setDogeDirectorySwitchContexts(contexts map[string]*tokenSwitchContext) {
	state := s.runtime.State()
	s.switchMu.Lock()
	if s.directoryRecoveryNotices == nil {
		s.directoryRecoveryNotices = make(map[string]*PublicDogeTokenSwitchPrompt)
	}
	previousContexts := cloneDogeDirectorySwitchContexts(s.directorySwitches)
	previousKeys := make(map[string]string, len(s.directorySwitches))
	for category, context := range s.directorySwitches {
		if context != nil {
			previousKeys[category] = context.key
		}
	}
	s.directorySwitches = make(map[string]*tokenSwitchContext, len(contexts))
	changed := len(previousKeys) != len(contexts)
	for category, context := range contexts {
		if context == nil {
			continue
		}
		s.directorySwitches[category] = context
		if previousKeys[category] != context.key {
			changed = true
		}
	}
	for category := range contexts {
		delete(s.directoryRecoveryNotices, category)
	}
	s.switchMu.Unlock()
	if state != nil {
		for category, previous := range previousContexts {
			if previous == nil || previous.directoryReason != dogeDirectoryFailureUnavailable || contexts[category] != nil {
				continue
			}
			activeID := strings.TrimSpace(state.Config.ActiveProfiles[category])
			activeIndex := config.FindProfileIndex(state.Config.Profiles, activeID)
			if activeID == "" || activeIndex < 0 || state.Config.Profiles[activeIndex].Category != category || config.FindProfileIndex(state.Config.Profiles, previous.profile.ID) < 0 {
				continue
			}
			recoveredTokens := recoveredDogeTokens(previous, state.Config)
			if len(recoveredTokens) == 0 {
				continue
			}
			candidates := availableFailoverProfiles(state.Config, category)
			notice := buildDogeDirectoryRecoveryNotice(state.Config, previous, recoveredTokens, candidates)
			s.switchMu.Lock()
			if existing := s.directoryRecoveryNotices[category]; existing == nil || existing.Key != notice.Key {
				s.directoryRecoveryNotices[category] = notice
				changed = true
			}
			s.switchMu.Unlock()
		}
	}
	for _, context := range contexts {
		if context != nil {
			state = s.runtime.State()
			if state != nil && state.Config.TokenSwitch.Mode == config.TokenSwitchModeAuto &&
				directoryTriggerEnabled(state.Config.TokenSwitch, context.directoryReason) &&
				directorySwitchContextApplies(state.Config, context) {
				current := *context
				current.tokens = append([]config.DogeToken(nil), state.Config.Doge.Tokens...)
				current.groups = append([]string(nil), state.Config.Doge.Groups...)
				current.candidateProfiles = directoryFailoverCandidates(state.Config, &current)
				prompt := s.applyTokenSwitchRound(publicDogeTokenSwitchPrompt(current), true)
				s.failoverMu.Lock()
				switched, previousID := s.tryAutomaticTokenSwitch(prompt)
				s.failoverMu.Unlock()
				if switched {
					s.runtime.ResetProfileHealth(previousID)
				}
			}
		}
	}
	if changed || len(contexts) > 0 {
		s.notifyStateChanged()
	}
}

// directorySwitchContextApplies 判断目录异常是否仍对应同步前的活动令牌。
// 目录删除同步后 Profile 和启用映射已经清理，此时仅允许该 missing 快照继续完成一次手动或自动故障切换。
func directorySwitchContextApplies(cfg config.AppConfig, context *tokenSwitchContext) bool {
	if context == nil {
		return false
	}
	category := context.profile.Category
	if cfg.ActiveProfiles[category] == context.profile.ID {
		return true
	}
	return context.directoryReason == dogeDirectoryFailureMissing &&
		cfg.ActiveProfiles[category] == "" && config.FindProfileIndex(cfg.Profiles, context.profile.ID) < 0
}

// switchPromptStates 同步清理已经不再触发的提示状态，并返回当前仍允许展示的状态。
func (s *DesktopService) switchPromptStates(activeKeys map[string]struct{}) map[string]bool {
	now := time.Now()
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	visible := make(map[string]bool, len(activeKeys))
	suppressedBases := make(map[string]struct{})
	for key := range s.switchPrompts {
		promptState := s.switchPrompts[key]
		_, active := activeKeys[key]
		if !active {
			// 失败状态刚恢复时保留五分钟抑制窗口，避免短暂恢复后再次连续失败立即弹窗。
			if promptState.dismissed && now.Before(promptState.suppressedUntil) {
				suppressedBases[switchPromptBaseKey(key)] = struct{}{}
				continue
			}
			if !promptState.dismissed || !now.Before(promptState.suppressedUntil) {
				delete(s.switchPrompts, key)
			}
			continue
		}
		if promptState.dismissed {
			// 用户取消后，只要同一失败状态仍在持续，就不因五分钟到期再次打扰。
			visible[key] = true
		}
	}
	for key := range activeKeys {
		if _, ok := s.switchPrompts[key]; !ok {
			s.switchPrompts[key] = &tokenSwitchPromptState{}
		}
		if _, suppressed := suppressedBases[switchPromptBaseKey(key)]; suppressed {
			visible[key] = true
		}
	}
	return visible
}

func switchPromptBaseKey(key string) string {
	if index := strings.LastIndex(key, "|"); index >= 0 {
		return key[:index]
	}
	return key
}

// publicDogeTokenSwitchPrompt 将内部 Profile 转为不含完整密钥的前端提示；保留旧名称以兼容提醒窗口绑定。
