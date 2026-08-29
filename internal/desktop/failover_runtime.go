package desktop

import (
	"fmt"
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/tasknotify"
)

// handleUpstreamResult 在当前活动令牌真正成功后结束该类别的故障轮次，并刷新已显示的手动故障提示。
// 自动切换成功后的通知继续保留到用户确认；这里只清理尝试集合，使后续新故障可以开始新一轮。
func (s *DesktopService) handleUpstreamResult(profileID, category string, status int, transportError bool) {
	if transportError || status >= 400 || profileID == "" || category == "" {
		return
	}
	state := s.runtime.State()
	if state == nil || state.Config.ActiveProfiles[category] != profileID {
		return
	}
	s.switchMu.Lock()
	_, clearedRound := s.switchRounds[category]
	clearedPrompt := false
	for key := range s.switchPrompts {
		if strings.HasPrefix(key, profileID+"|") {
			delete(s.switchPrompts, key)
			clearedPrompt = true
		}
	}
	if clearedRound {
		delete(s.switchRounds, category)
	}
	s.switchMu.Unlock()
	// 手动模式没有 switchRounds；仍须在成功响应清掉已显示的故障提示并刷新独立窗口。
	if clearedRound || clearedPrompt {
		s.notifyStateChanged()
	}
}

// tryAutomaticTokenSwitch 在同一故障轮次中依次尝试未尝试的候选。
// 候选切换失败也会计入尝试集合；全部候选耗尽后只生成一次停止提示，不再回到列表开头。
func (s *DesktopService) tryAutomaticTokenSwitch(prompt *PublicDogeTokenSwitchPrompt) (bool, string) {
	if prompt == nil {
		return false, ""
	}
	s.ensureTokenSwitchRound(prompt.Category)
	s.resumeTokenSwitchRound(prompt)
	s.markTokenSwitchAttempt(prompt.Category, prompt.CurrentProfileID)
	for _, candidate := range prompt.Candidates {
		if !candidate.Selectable || candidate.ProfileID == "" {
			continue
		}
		if s.tokenSwitchAttempted(prompt.Category, candidate.ProfileID) {
			continue
		}
		s.markTokenSwitchAttempt(prompt.Category, candidate.ProfileID)
		if err := s.switchProfile(prompt.Category, prompt.CurrentProfileID, candidate.ProfileID, tokenSwitchCurrentWasRemoved(s.runtime.State(), prompt)); err != nil {
			continue
		}
		s.recordTokenSwitch(prompt.Category, prompt.CurrentName, candidate.Name, historyFailureMessage(prompt))
		s.clearSwitchPrompt(prompt.Key)
		s.setAutoSwitchNotice(prompt, candidate.Name)
		s.enqueueTaskNotificationEvent(tasknotify.EventTokenAutoSwitched, fmt.Sprintf("%s\x00%s\x00%d", prompt.Key, candidate.ProfileID, time.Now().UnixNano()), tasknotify.EventDetails{
			OccurredAt: time.Now(), Category: prompt.Category, FromGroup: prompt.CurrentGroup, ToGroup: candidate.Group,
		})
		return true, prompt.CurrentProfileID
	}

	s.stopTokenSwitchRound(prompt)
	return false, ""
}

// resumeTokenSwitchRound 只在真正准备自动尝试且出现未尝试候选时恢复已停止轮次。
// 状态读取和窗口渲染不得修改轮次，避免配置保存通知与自动执行之间短暂显示错误模式。
func (s *DesktopService) resumeTokenSwitchRound(prompt *PublicDogeTokenSwitchPrompt) {
	if prompt == nil {
		return
	}
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	round := s.switchRounds[prompt.Category]
	if round == nil || !round.Stopped {
		return
	}
	hasCandidate := false
	for _, candidate := range prompt.Candidates {
		if candidate.Selectable && candidate.ProfileID != "" {
			hasCandidate = true
			break
		}
	}
	if !hasCandidate {
		return
	}
	round.Stopped = false
	round.StoppedAt = time.Time{}
	round.StopMessage = ""
	delete(s.autoSwitchNotices, prompt.Category)
}

func (s *DesktopService) ensureTokenSwitchRound(category string) *tokenSwitchRound {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	if s.switchRounds == nil {
		s.switchRounds = make(map[string]*tokenSwitchRound)
	}
	round := s.switchRounds[category]
	if round == nil {
		round = &tokenSwitchRound{AttemptedIDs: make(map[string]struct{})}
		s.switchRounds[category] = round
	}
	return round
}

func (s *DesktopService) markTokenSwitchAttempt(category, profileID string) {
	if strings.TrimSpace(profileID) == "" {
		return
	}
	round := s.ensureTokenSwitchRound(category)
	s.switchMu.Lock()
	if round.AttemptedIDs == nil {
		round.AttemptedIDs = make(map[string]struct{})
	}
	round.AttemptedIDs[profileID] = struct{}{}
	s.switchMu.Unlock()
}

func (s *DesktopService) tokenSwitchAttempted(category, profileID string) bool {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	round := s.switchRounds[category]
	if round == nil {
		return false
	}
	_, ok := round.AttemptedIDs[profileID]
	return ok
}

func (s *DesktopService) recordTokenSwitch(category, fromName, toName, failureMessage string) {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	round := s.switchRounds[category]
	if round == nil {
		return
	}
	round.History = append(round.History, PublicDogeTokenSwitchHistory{
		FromName: fromName, ToName: toName, SwitchedAt: time.Now().Format("2006-01-02 15:04:05"), FailureMessage: failureMessage,
	})
}

func (s *DesktopService) stopTokenSwitchRound(prompt *PublicDogeTokenSwitchPrompt) {
	if prompt == nil {
		return
	}
	switchMu := &s.switchMu
	switchMu.Lock()
	if s.switchRounds == nil {
		s.switchRounds = make(map[string]*tokenSwitchRound)
	}
	round := s.switchRounds[prompt.Category]
	if round == nil {
		round = &tokenSwitchRound{AttemptedIDs: make(map[string]struct{})}
		s.switchRounds[prompt.Category] = round
	}
	if !round.Stopped {
		round.History = append(round.History, PublicDogeTokenSwitchHistory{
			FromName:       prompt.CurrentName,
			SwitchedAt:     time.Now().Format("2006-01-02 15:04:05"),
			FailureMessage: historyFailureMessage(prompt),
		})
	}
	round.Stopped = true
	round.StoppedAt = time.Now()
	round.StopMessage = fmt.Sprintf("当前类别暂无可用令牌，已停止自动切换，避免重复使用故障令牌。")
	notice := *prompt
	notice.Mode = "auto"
	notice.Stopped = true
	notice.StoppedAt = round.StoppedAt.Format("2006-01-02 15:04:05")
	notice.StopMessage = round.StopMessage
	notice.Message = "本轮令牌均已尝试，自动切换已停止。"
	notice.Candidates = nil
	notice.SwitchHistory = append([]PublicDogeTokenSwitchHistory(nil), round.History...)
	if s.autoSwitchNotices == nil {
		s.autoSwitchNotices = make(map[string]*PublicDogeTokenSwitchPrompt)
	}
	s.autoSwitchNotices[prompt.Category] = &notice
	switchMu.Unlock()
	s.enqueueTaskNotificationEvent(tasknotify.EventTokenAutoSwitchFailed, fmt.Sprintf("%s\x00%d", prompt.Key, time.Now().UnixNano()), tasknotify.EventDetails{
		OccurredAt: time.Now(), Category: prompt.Category, FromGroup: prompt.CurrentGroup, ToGroup: "无可用分组",
	})
}

func historyFailureMessage(prompt *PublicDogeTokenSwitchPrompt) string {
	if prompt == nil {
		return "上游请求异常"
	}
	switch prompt.FailureKind {
	case "auth":
		return fmt.Sprintf("连续 %d 次返回 HTTP %d", prompt.FailureCount, prompt.FailureStatus)
	case "directory":
		if prompt.Message != "" {
			return prompt.Message
		}
		return "令牌目录状态异常"
	default:
		return fmt.Sprintf("%d 分钟内累计 %d 次上游异常", promptFailureWindow(prompt), prompt.FailureCount)
	}
}

func promptFailureWindow(prompt *PublicDogeTokenSwitchPrompt) int {
	if prompt == nil || prompt.FailureWindowMinutes <= 0 {
		return config.DefaultUpstreamFailureWindowMinutes
	}
	return prompt.FailureWindowMinutes
}

func (s *DesktopService) clearSwitchPrompt(key string) {
	s.switchMu.Lock()
	delete(s.switchPrompts, key)
	// 目录失效快照不能在切换成功后立即删除：下一次同步需要用它对比旧状态，
	// 才能在令牌状态或分组恢复时生成独立提醒。setDogeDirectorySwitchContexts
	// 会在下一次同步中按最新目录替换或清理该快照。
	for category, notice := range s.directoryRecoveryNotices {
		if notice != nil && notice.Key == key {
			delete(s.directoryRecoveryNotices, category)
		}
	}
	s.switchMu.Unlock()
}

// currentTokenSwitchPrompts 返回每个类别当前应显示的独立令牌状态。
// 同类别自动切换结果覆盖该类别的手动提示，其他类别互不覆盖；所有状态只在本次运行内保留。
func (s *DesktopService) currentTokenSwitchPrompts() map[string]*PublicDogeTokenSwitchPrompt {
	prompts := s.buildTokenSwitchPrompts()
	state := s.runtime.State()
	if state == nil || state.Config.TokenSwitch.Mode != config.TokenSwitchModeAuto {
		return prompts
	}
	s.switchMu.Lock()
	for category, notice := range s.autoSwitchNotices {
		if notice == nil {
			continue
		}
		if prompt := prompts[category]; prompt != nil && prompt.FailureKind == "directory_recovered" {
			continue
		}
		clone := *notice
		clone.SwitchHistory = append([]PublicDogeTokenSwitchHistory(nil), notice.SwitchHistory...)
		prompts[category] = &clone
	}
	s.switchMu.Unlock()
	return prompts
}

func firstTokenSwitchPrompt(prompts map[string]*PublicDogeTokenSwitchPrompt) *PublicDogeTokenSwitchPrompt {
	for _, category := range config.Categories {
		if prompt := prompts[category]; prompt != nil {
			return prompt
		}
	}
	return nil
}

// setAutoSwitchNotice 保存自动切换成功结果，供独立令牌提醒窗口渲染。
// prompt 来自切换前的候选快照，targetName 必须是经过统一格式化的目标名称。
func (s *DesktopService) setAutoSwitchNotice(prompt *PublicDogeTokenSwitchPrompt, targetName string) {
	if prompt == nil {
		return
	}
	notice := *prompt
	notice.Mode = "auto"
	notice.SwitchedToName = strings.TrimSpace(targetName)
	notice.Message = autoSwitchMessage(prompt, notice.SwitchedToName)
	notice.Candidates = nil
	s.switchMu.Lock()
	if round := s.switchRounds[prompt.Category]; round != nil {
		notice.SwitchHistory = append([]PublicDogeTokenSwitchHistory(nil), round.History...)
		notice.Stopped = round.Stopped
		notice.StoppedAt = ""
		notice.StopMessage = round.StopMessage
	}
	if s.autoSwitchNotices == nil {
		s.autoSwitchNotices = make(map[string]*PublicDogeTokenSwitchPrompt)
	}
	s.autoSwitchNotices[prompt.Category] = &notice
	s.switchMu.Unlock()
}

func autoSwitchMessage(prompt *PublicDogeTokenSwitchPrompt, targetName string) string {
	currentName := prompt.CurrentName
	if currentName == "" {
		currentName = "当前代理 API"
	}
	if targetName == "" {
		targetName = "下一个可用令牌"
	}
	var failure string
	switch prompt.FailureKind {
	case "auth":
		failure = fmt.Sprintf("连续 %d 次返回 HTTP %d，已达到故障阈值。", prompt.FailureCount, prompt.FailureStatus)
	case "directory":
		failure = "已从最新令牌目录中消失，已达到故障阈值。"
	default:
		failure = fmt.Sprintf("在设定的异常统计窗口内出现 %d 次上游异常，已达到故障阈值。", prompt.FailureCount)
	}
	return fmt.Sprintf("当前 %s %s\n已自动切换至 %s。", currentName, failure, targetName)
}
