package desktop

import (
	"fmt"
	"strconv"
	"strings"

	"codexrelay/internal/config"
)

func directoryTriggerEnabled(settings config.TokenSwitchSettings, reason string) bool {
	if reason == dogeDirectoryFailureMissing {
		return settings.TriggerDirectoryMissing
	}
	return settings.TriggerDirectoryInvalid
}

func (s *DesktopService) failoverCandidates(category, currentID string, loop bool) []config.Profile {
	state := s.runtime.State()
	if state == nil {
		return nil
	}
	return failoverCandidatesFromConfig(state.Config, category, currentID, loop)
}

// failoverCandidatesFromConfig 按给定配置快照计算一个类别的后续候选。
// 目录同步在删除当前 Profile 前调用它，既保留原列表位置，又使用最新目录过滤同时消失或失效的候选。
func failoverCandidatesFromConfig(cfg config.AppConfig, category, currentID string, loop bool) []config.Profile {
	ordered := config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)[category]
	byID := make(map[string]config.Profile, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		if profile.Category == category {
			byID[profile.ID] = profile
		}
	}
	start := -1
	for index, id := range ordered {
		if id == currentID {
			start = index
			break
		}
	}
	if start < 0 {
		start = -1
	}
	limit := len(ordered)
	if !loop && start >= 0 {
		limit -= start + 1
	}
	result := make([]config.Profile, 0, len(ordered))
	for step := 1; step <= limit; step++ {
		index := start + step
		if loop {
			index %= len(ordered)
		}
		if index < 0 || index >= len(ordered) || (loop && index == start) {
			break
		}
		profile, ok := byID[ordered[index]]
		if !ok || !failoverProfileAvailable(cfg, profile) {
			continue
		}
		if cfg.TokenSwitch.Mode == config.TokenSwitchModeAuto && profile.SkipAutoSwitch {
			continue
		}
		result = append(result, profile)
	}
	return result
}

// directoryFailoverCandidates 使用当前配置重算目录异常候选，仅从同步前顺序保留已删除当前项的位置。
// 模式、循环、跳过状态、新增 Profile 和最新二狗子目录都以调用时快照为准，不能沿用发生异常时的候选列表。
func directoryFailoverCandidates(cfg config.AppConfig, context *tokenSwitchContext) []config.Profile {
	if context == nil {
		return nil
	}
	candidateConfig := config.Clone(cfg)
	category := context.profile.Category
	currentID := context.profile.ID
	if context.directoryReason == dogeDirectoryFailureMissing {
		if config.FindProfileIndex(candidateConfig.Profiles, currentID) < 0 {
			candidateConfig.Profiles = append(candidateConfig.Profiles, context.profile)
		}
		currentOrder := config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)[category]
		anchoredOrder := insertMissingFailoverAnchor(currentOrder, context.failoverOrder, currentID)
		if candidateConfig.FailoverOrder == nil {
			candidateConfig.FailoverOrder = make(map[string][]string)
		}
		candidateConfig.FailoverOrder[category] = anchoredOrder
	}
	return failoverCandidatesFromConfig(candidateConfig, category, currentID, candidateConfig.TokenSwitch.Loop)
}

// insertMissingFailoverAnchor 把已删除当前项放回最新可见顺序中的原相对位置。
// 优先使用仍存在的前一项，其次使用后一项；两侧都不存在时按原索引落位，使列表首尾语义保持稳定。
func insertMissingFailoverAnchor(currentOrder, previousOrder []string, currentID string) []string {
	result := append([]string(nil), currentOrder...)
	for _, id := range result {
		if id == currentID {
			return result
		}
	}
	previousIndex := -1
	for index, id := range previousOrder {
		if id == currentID {
			previousIndex = index
			break
		}
	}
	positions := make(map[string]int, len(result))
	for index, id := range result {
		positions[id] = index
	}
	insertAt := -1
	if previousIndex >= 0 {
		for index := previousIndex - 1; index >= 0; index-- {
			if position, ok := positions[previousOrder[index]]; ok {
				insertAt = position + 1
				break
			}
		}
		if insertAt < 0 {
			for index := previousIndex + 1; index < len(previousOrder); index++ {
				if position, ok := positions[previousOrder[index]]; ok {
					insertAt = position
					break
				}
			}
		}
	}
	if insertAt < 0 {
		insertAt = previousIndex
		if insertAt < 0 || insertAt > len(result) {
			insertAt = len(result)
		}
	}
	result = append(result, "")
	copy(result[insertAt+1:], result[insertAt:])
	result[insertAt] = currentID
	return result
}

func availableFailoverProfiles(cfg config.AppConfig, category string) []config.Profile {
	order := config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)[category]
	byID := make(map[string]config.Profile, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		if profile.Category == category {
			byID[profile.ID] = profile
		}
	}
	result := make([]config.Profile, 0, len(order))
	for _, id := range order {
		profile, ok := byID[id]
		if ok && failoverProfileAvailable(cfg, profile) {
			result = append(result, profile)
		}
	}
	return result
}

func failoverProfileAvailable(cfg config.AppConfig, profile config.Profile) bool {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.APIKey) == "" {
		return false
	}
	if profile.Source != config.SourceDoge {
		return true
	}
	if profile.RemoteTokenID <= 0 {
		return false
	}
	for _, token := range cfg.Doge.Tokens {
		if token.ID == profile.RemoteTokenID {
			return dogeTokenSwitchable(token, cfg.Doge.Groups)
		}
	}
	return false
}

func dogeTokenForProfile(cfg config.AppConfig, profile config.Profile) config.DogeToken {
	if profile.Source != config.SourceDoge || profile.RemoteTokenID <= 0 {
		return config.DogeToken{}
	}
	for _, token := range cfg.Doge.Tokens {
		if token.ID == profile.RemoteTokenID {
			return token
		}
	}
	return config.DogeToken{ID: profile.RemoteTokenID, Name: profile.Name, Category: profile.Category}
}

func failoverSourceLabel(source string) string {
	if source == config.SourceDoge {
		return "二狗子 API"
	}
	return "自定义 API"
}

func dogeFailoverProfileID(tokenID int64) string {
	return fmt.Sprintf("doge-token:%d", tokenID)
}

func dogeTokenIDFromFailoverProfileID(profileID string) (int64, bool) {
	if !strings.HasPrefix(profileID, "doge-token:") {
		return 0, false
	}
	tokenID, err := strconv.ParseInt(strings.TrimPrefix(profileID, "doge-token:"), 10, 64)
	return tokenID, err == nil && tokenID > 0
}
