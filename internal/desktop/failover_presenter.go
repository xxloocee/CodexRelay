package desktop

import (
	"fmt"
	"strings"

	"codexrelay/internal/config"
)

func publicDogeTokenSwitchPrompt(context tokenSwitchContext) *PublicDogeTokenSwitchPrompt {
	currentName := strings.TrimSpace(context.profile.Name)
	if currentName == "" {
		currentName = context.token.Name
	}
	if currentName == "" {
		currentName = "当前代理 API"
	}
	currentGroup := dogeTokenDisplayGroup(context.token)
	currentName = formatNonHomeProfileName(currentName, context.profile.Source, currentGroup, context.token.GroupRatio)
	candidates := make([]PublicDogeTokenSwitchCandidate, 0)
	for _, profile := range context.candidateProfiles {
		if profile.ID == context.profile.ID {
			continue
		}
		token := context.token
		if profile.Source == config.SourceDoge {
			for _, candidateToken := range context.tokens {
				if candidateToken.ID == profile.RemoteTokenID {
					token = candidateToken
					break
				}
			}
		}
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			name = "代理 API " + profile.ID
		}
		group := ""
		ratio := float64(0)
		tokenID := int64(0)
		if profile.Source == config.SourceDoge {
			tokenID = profile.RemoteTokenID
			group = dogeTokenDisplayGroup(token)
			ratio = token.GroupRatio
		}
		name = formatNonHomeProfileName(name, profile.Source, group, ratio)
		candidates = append(candidates, PublicDogeTokenSwitchCandidate{
			TokenID: tokenID, ProfileID: profile.ID, Name: name, Source: failoverSourceLabel(profile.Source),
			Group: group, Ratio: ratio, Selectable: true,
		})
	}
	failureWindow := context.failureWindowMinutes
	if failureWindow <= 0 {
		failureWindow = config.DefaultUpstreamFailureWindowMinutes
	}
	failureMessage := fmt.Sprintf("当前代理 API“%s”在 %d 分钟内出现 %d 次上游异常，是否切换到列表中的下一个可用项？", currentName, failureWindow, context.failureCount)
	if context.failureKind == "auth" {
		failureMessage = fmt.Sprintf("当前代理 API“%s”连续 %d 次返回 HTTP %d，是否切换到列表中的下一个可用项？", currentName, context.failureCount, context.failureStatus)
	} else if context.failureKind == "directory" {
		if context.directoryReason == dogeDirectoryFailureMissing {
			failureMessage = fmt.Sprintf("当前令牌“%s”已从最新令牌目录中消失，是否切换到列表中的下一个可用项？", currentName)
		} else {
			failureMessage = fmt.Sprintf("当前令牌“%s”在最新令牌目录中已失效，是否切换到列表中的下一个可用项？", currentName)
		}
	}
	return &PublicDogeTokenSwitchPrompt{
		Key: context.key, Category: context.profile.Category, Mode: "manual", FailureKind: context.failureKind,
		FailureCount: context.failureCount, FailureStatus: context.failureStatus, FailureWindowMinutes: failureWindow, CurrentTokenID: context.token.ID, CurrentProfileID: context.profile.ID,
		CurrentName: currentName, CurrentGroup: currentGroup, CurrentRatio: context.token.GroupRatio,
		Message: failureMessage, Candidates: candidates,
	}
}

func publicDogeTokenSwitchCandidates(cfg config.AppConfig, profiles []config.Profile, currentID string) []PublicDogeTokenSwitchCandidate {
	candidates := make([]PublicDogeTokenSwitchCandidate, 0, len(profiles))
	for _, profile := range profiles {
		if profile.ID == currentID {
			continue
		}
		token := dogeTokenForProfile(cfg, profile)
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			name = "代理 API " + profile.ID
		}
		group := ""
		ratio := float64(0)
		tokenID := int64(0)
		if profile.Source == config.SourceDoge {
			tokenID = profile.RemoteTokenID
			group = dogeTokenDisplayGroup(token)
			ratio = token.GroupRatio
		}
		name = formatNonHomeProfileName(name, profile.Source, group, ratio)
		candidates = append(candidates, PublicDogeTokenSwitchCandidate{
			TokenID: tokenID, ProfileID: profile.ID, Name: name, Source: failoverSourceLabel(profile.Source),
			Group: group, Ratio: ratio, Selectable: true,
		})
	}
	return candidates
}

// formatNonHomeProfileName 统一托盘、提醒窗口和统计页等非主页位置的 Profile 名称。
// 主界面与编辑器仍使用原始名称；自定义 API 没有二狗子分组和倍率，只追加固定来源说明。
func formatNonHomeProfileName(name, source, group string, ratio float64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "未命名令牌"
	}
	if source == config.SourceCustom {
		return fmt.Sprintf("%s（自定义 API）", name)
	}
	return formatDogeProfileName(name, group, ratio)
}

// formatDogeProfileName 统一非主页位置的二狗子令牌名称；主界面列表仍分别展示名称、分组和倍率标签。
func formatDogeProfileName(name, group string, ratio float64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "未命名令牌"
	}
	group = strings.TrimSpace(group)
	if group == "" {
		return name
	}
	if ratio > 0 {
		return fmt.Sprintf("%s (%s·%s)", name, group, formatDogeRatio(ratio))
	}
	return fmt.Sprintf("%s (%s)", name, group)
}
