package desktop

import (
	"fmt"
	"sort"
	"strconv"

	"codexrelay/internal/config"
)

// CompleteOnboarding 结束首次启动引导并启用便携数据持久化；跳过和绑定成功都调用此方法。
func (s *DesktopService) CompleteOnboarding() error {
	if err := s.runtime.ActivatePortablePersistence(); err != nil {
		return fmt.Errorf("保存首次初始化数据: %w", err)
	}
	s.setNeedsOnboarding(false)
	return nil
}

// buildDogeTokenSwitchPrompt 保留原绑定名称，实际构建所有来源共用的令牌切换提示。
func (s *DesktopService) buildDogeTokenSwitchPrompt() *PublicDogeTokenSwitchPrompt {
	return s.buildTokenSwitchPrompt()
}

// buildTokenSwitchPrompt 保留单条提示入口，供现有调用和测试读取类别顺序中的第一条提示。
func (s *DesktopService) buildTokenSwitchPrompt() *PublicDogeTokenSwitchPrompt {
	return firstTokenSwitchPrompt(s.buildTokenSwitchPrompts())
}

// buildTokenSwitchPrompts 根据运行时健康快照和二狗子目录状态为每个类别构建一条提示。
// 候选只限制当前 API 类别，来源不参与筛选；顺序来自该类别的 FailoverOrder。
// 用户取消后的抑制状态只保存在内存中，失败状态恢复后会被清理。
func (s *DesktopService) buildTokenSwitchPrompts() map[string]*PublicDogeTokenSwitchPrompt {
	result := make(map[string]*PublicDogeTokenSwitchPrompt)
	state := s.runtime.State()
	if state == nil {
		return result
	}

	snapshots := s.runtime.HealthSnapshots()
	type triggeredContext struct {
		context  tokenSwitchContext
		priority int
	}
	contexts := make([]triggeredContext, 0, len(snapshots))
	activeKeys := make(map[string]struct{}, len(snapshots))
	for _, directoryContext := range s.dogeDirectorySwitchContexts() {
		if directoryContext != nil && directoryTriggerEnabled(state.Config.TokenSwitch, directoryContext.directoryReason) &&
			directorySwitchContextApplies(state.Config, directoryContext) {
			directoryContext.tokens = append([]config.DogeToken(nil), state.Config.Doge.Tokens...)
			directoryContext.groups = append([]string(nil), state.Config.Doge.Groups...)
			directoryContext.candidateProfiles = directoryFailoverCandidates(state.Config, directoryContext)
			activeKeys[directoryContext.key] = struct{}{}
			contexts = append(contexts, triggeredContext{context: *directoryContext, priority: -1})
		}
	}
	for _, health := range snapshots {
		failureKind, failureCount, priority := "", 0, 0
		switch {
		case health.AuthTriggered:
			failureKind, failureCount, priority = "auth", health.AuthFailures, 0
		case health.UpstreamTriggered:
			failureKind, failureCount, priority = "upstream", health.UpstreamFailures, 1
		default:
			continue
		}
		profileIndex := config.FindProfileIndex(state.Config.Profiles, health.ProfileID)
		if profileIndex < 0 {
			continue
		}
		profile := state.Config.Profiles[profileIndex]
		if state.Config.ActiveProfiles[profile.Category] != profile.ID {
			continue
		}
		key := profile.ID + "|" + failureKind + "|" + strconv.Itoa(health.LastStatus) + "|" + strconv.FormatUint(health.TriggerGeneration, 10)
		activeKeys[key] = struct{}{}
		ctx := tokenSwitchContext{
			key: key, failureKind: failureKind, failureCount: failureCount, failureStatus: health.LastStatus,
			failureWindowMinutes: state.Config.TokenSwitch.UpstreamFailureWindowMinutes,
			health:               health, profile: profile, token: dogeTokenForProfile(state.Config, profile),
			tokens: append([]config.DogeToken(nil), state.Config.Doge.Tokens...), groups: append([]string(nil), state.Config.Doge.Groups...),
			candidateProfiles: s.failoverCandidates(profile.Category, profile.ID, state.Config.TokenSwitch.Loop),
		}
		contexts = append(contexts, triggeredContext{context: ctx, priority: priority})
	}

	switchPromptStates := s.switchPromptStates(activeKeys)
	sort.Slice(contexts, func(i, j int) bool {
		if contexts[i].priority != contexts[j].priority {
			return contexts[i].priority < contexts[j].priority
		}
		return contexts[i].context.key < contexts[j].context.key
	})
	for _, item := range contexts {
		if dismissed, ok := switchPromptStates[item.context.key]; ok && dismissed {
			continue
		}
		category := item.context.profile.Category
		if result[category] == nil {
			result[category] = s.applyTokenSwitchRound(publicDogeTokenSwitchPrompt(item.context), state.Config.TokenSwitch.Mode == config.TokenSwitchModeAuto)
		}
	}
	s.switchMu.Lock()
	for category, notice := range s.directoryRecoveryNotices {
		if notice == nil {
			continue
		}
		if state.Config.ActiveProfiles[category] != notice.CurrentProfileID || result[category] != nil {
			continue
		}
		clone := *notice
		clone.Candidates = append([]PublicDogeTokenSwitchCandidate(nil), notice.Candidates...)
		result[category] = &clone
	}
	s.switchMu.Unlock()
	return result
}

// applyTokenSwitchRound 只在自动模式下合并当前类别的历史并过滤本轮已尝试候选。
// 手动提示不属于自动故障轮次，用户每次收到提示时都能按当前列表选择任意可用候选。
func (s *DesktopService) applyTokenSwitchRound(prompt *PublicDogeTokenSwitchPrompt, automatic bool) *PublicDogeTokenSwitchPrompt {
	if prompt == nil {
		return nil
	}
	if !automatic {
		return prompt
	}
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	round := s.switchRounds[prompt.Category]
	if round == nil {
		return prompt
	}
	filtered := make([]PublicDogeTokenSwitchCandidate, 0, len(prompt.Candidates))
	for _, candidate := range prompt.Candidates {
		if _, attempted := round.AttemptedIDs[candidate.ProfileID]; attempted {
			continue
		}
		filtered = append(filtered, candidate)
	}
	prompt.Candidates = filtered
	prompt.SwitchHistory = append([]PublicDogeTokenSwitchHistory(nil), round.History...)
	prompt.Stopped = round.Stopped
	if !round.Stopped && len(filtered) == 0 {
		prompt.Stopped = true
	}
	if round.Stopped || prompt.Stopped {
		prompt.StopMessage = round.StopMessage
		if prompt.StopMessage == "" {
			prompt.StopMessage = "当前类别暂无可用令牌，已停止自动切换，避免重复使用故障令牌。"
		}
	}
	return prompt
}
