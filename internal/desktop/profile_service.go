package desktop

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"codexrelay/internal/config"
	"codexrelay/internal/desktop/clientconfig"
)

func (s *DesktopService) ReorderProfiles(ids []string) error {
	return s.updateConfig(func(cfg *config.AppConfig) error {
		ordered, err := config.OrderProfiles(cfg.Profiles, ids)
		if err != nil {
			return err
		}
		cfg.Profiles = ordered
		return nil
	})
}

// ReorderDogeTokens 按二狗子远端令牌 ID 保存主页顺序；名称、分组和 API 密钥只用于展示或访问，
// 不参与令牌身份定位。
func (s *DesktopService) ReorderDogeTokens(orderKeys []string) error {
	return s.updateConfig(func(cfg *config.AppConfig) error {
		known := make(map[string]struct{}, len(cfg.Doge.Tokens))
		for _, token := range cfg.Doge.Tokens {
			key := dogeTokenOrderKey(token)
			if key == "" {
				continue
			}
			known[key] = struct{}{}
		}
		seen := make(map[string]struct{}, len(orderKeys))
		next := make([]string, 0, len(known))
		for _, key := range orderKeys {
			key = strings.TrimSpace(key)
			if key == "" {
				return errors.New("二狗子令牌排序 ID 不能为空")
			}
			if _, ok := known[key]; !ok {
				return errors.New("二狗子令牌排序包含未知 ID")
			}
			if _, ok := seen[key]; ok {
				return errors.New("二狗子令牌排序包含重复 ID")
			}
			seen[key] = struct{}{}
			next = append(next, key)
		}
		for _, token := range cfg.Doge.Tokens {
			key := dogeTokenOrderKey(token)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				next = append(next, key)
			}
		}
		cfg.Doge.TokenOrder = next
		cfg.Doge.Tokens = orderDogeTokens(next, cfg.Doge.Tokens)
		return nil
	})
}

// SetDogeTokenCategories 校验整批选择后，将待导入令牌一次性创建为本地 Profile。
// 新 Profile 按本批选择顺序追加到所属类别末尾；整批任一项无效时不保存部分结果。
func (s *DesktopService) SetDogeTokenCategories(assignments []DogeTokenCategoryInput) error {
	if len(assignments) == 0 {
		return errors.New("至少选择一个二狗子令牌类别")
	}
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		byID := make(map[int64]int, len(cfg.Doge.Tokens))
		for index := range cfg.Doge.Tokens {
			byID[cfg.Doge.Tokens[index].ID] = index
		}
		imported := make(map[int64]struct{})
		for _, profile := range cfg.Profiles {
			if profile.Source == config.SourceDoge && profile.RemoteTokenID > 0 {
				imported[profile.RemoteTokenID] = struct{}{}
			}
		}
		seen := make(map[int64]struct{}, len(assignments))
		for _, assignment := range assignments {
			if assignment.ID <= 0 {
				return errors.New("二狗子令牌 ID 无效")
			}
			if _, ok := seen[assignment.ID]; ok {
				return fmt.Errorf("二狗子令牌 %d 重复选择类别", assignment.ID)
			}
			seen[assignment.ID] = struct{}{}
			if !config.IsCategory(assignment.Category) {
				return fmt.Errorf("二狗子令牌 %d 的存放类别无效", assignment.ID)
			}
			index, ok := byID[assignment.ID]
			if !ok {
				return fmt.Errorf("二狗子令牌 %d 不存在，请先刷新目录", assignment.ID)
			}
			if _, exists := imported[assignment.ID]; exists {
				return fmt.Errorf("二狗子令牌 %d 已导入，不能通过同步弹窗修改类别", assignment.ID)
			}
			remote := cfg.Doge.Tokens[index]
			remote.Key = normalizeDogeAPIKey(remote.Key)
			if !isCompleteDogeAPIKey(remote.Key) {
				return fmt.Errorf("二狗子令牌 %d 缺少完整 API 密钥，请先手动同步", assignment.ID)
			}
		}
		for _, assignment := range assignments {
			index := byID[assignment.ID]
			cfg.Doge.Tokens[index].Category = assignment.Category
			cfg.Profiles = append(cfg.Profiles, newDogeProfile(cfg, cfg.Doge.Tokens[index], assignment.Category))
		}
		cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
		return nil
	}); err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
}

// newDogeProfile 只负责把已经通过类别和完整密钥校验的目录令牌转换为本地 Profile。
// 调用方必须在同一个配置事务中保存，避免令牌类别与 Profile 顺序分离。
func newDogeProfile(cfg *config.AppConfig, remote config.DogeToken, category string) config.Profile {
	name := strings.TrimSpace(remote.Name)
	if name == "" {
		name = "二狗子令牌 " + strconv.FormatInt(remote.ID, 10)
	}
	note := strings.TrimSpace(remote.Note)
	if note == "" {
		note = dogeTokenNote(remote)
	}
	return config.Profile{
		ID: config.NewProfileID(), Source: config.SourceDoge, Category: category, Name: name,
		BaseURL: strings.TrimRight(cfg.Doge.BaseURL, "/") + "/v1", APIKey: normalizeDogeAPIKey(remote.Key),
		Note: note, RemoteTokenID: remote.ID,
	}
}

// SaveProfile 新增或更新一个本地 Profile，并规范化其类别故障顺序。
// 活动 Profile 的客户端渲染字段变更会先同步已接管的外部配置，再提交本地状态。
func (s *DesktopService) SaveProfile(input ProfileInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.Note = strings.TrimSpace(input.Note)
	input.DefaultModel = strings.TrimSpace(input.DefaultModel)
	input.DogeGroup = strings.TrimSpace(input.DogeGroup)
	if input.Headers == nil {
		input.Headers = map[string]string{}
	}
	modelsOmitted := input.Models == nil
	dogeGroupUpdated := input.DogeGroup != ""
	if dogeGroupUpdated {
		state := s.runtime.State()
		if state == nil {
			return errors.New("程序尚未初始化")
		}
		preflight := config.Clone(state.Config)
		preflightInput := input
		_, prospective, existing, err := applyProfileInput(&preflight, &preflightInput, modelsOmitted)
		if err != nil {
			return err
		}
		if !existing || prospective.Source != config.SourceDoge || prospective.RemoteTokenID <= 0 {
			return errors.New("只有已导入的二狗子令牌可以修改远端分组")
		}
		if err := s.updateDogeTokenGroup(prospective.RemoteTokenID, input.DogeGroup); err != nil {
			return err
		}
	}
	if err := s.saveProfile(input, modelsOmitted); err != nil {
		if dogeGroupUpdated {
			s.handleHealthChanged()
			return fmt.Errorf("远端分组已修改，但本地 Profile 保存失败，请手动同步确认: %w", err)
		}
		return err
	}
	return nil
}

func (s *DesktopService) saveProfile(input ProfileInput, modelsOmitted bool) error {
	s.clientConfigMu.Lock()
	err := func() error {
		defer s.clientConfigMu.Unlock()
		state := s.runtime.State()
		if state == nil {
			return errors.New("程序尚未初始化")
		}
		next := config.Clone(state.Config)
		previous, prospective, existing, err := applyProfileInput(&next, &input, modelsOmitted)
		if err != nil {
			return err
		}

		var configResult clientconfig.ConfigureResult
		shouldSync := existing && previous.Category == prospective.Category &&
			state.Config.ActiveProfiles[previous.Category] == previous.ID &&
			!sameClientRenderedProfile(previous, prospective)
		if shouldSync && clientconfig.Supports(prospective.Category) {
			entry := state.Config.ClientConfigs[prospective.Category]
			if !entry.SkipConfigReplacement {
				status, inspectErr := clientconfig.Inspect(state.Config, prospective.Category)
				if inspectErr != nil {
					return fmt.Errorf("检查客户端配置失败: %w", inspectErr)
				}
				if status.Status == "error" {
					return fmt.Errorf("检查客户端配置失败: %s", status.Error)
				}
				if status.Configured {
					configResult, err = clientconfig.ConfigureWithResult(next, prospective.Category, prospective.ID)
					if err != nil {
						return fmt.Errorf("更新客户端配置失败: %w", err)
					}
				}
			}
		}

		_, err = s.runtime.UpdateConfig(func(cfg *config.AppConfig) error {
			if existing {
				currentIndex := config.FindProfileIndex(cfg.Profiles, input.ID)
				if currentIndex < 0 || !sameProfileForSave(previous, cfg.Profiles[currentIndex]) {
					return errors.New("代理 API 已被并发修改，请重试")
				}
			}
			_, committed, committedExisting, commitErr := applyProfileInput(cfg, &input, modelsOmitted)
			if commitErr != nil {
				return commitErr
			}
			if committedExisting != existing || (existing && (committed.ID != prospective.ID || (configResult.Rollback != nil && !sameClientRenderedProfile(prospective, committed)))) {
				return errors.New("代理 API 已被并发修改，请重试")
			}
			return nil
		})
		if err != nil {
			if configResult.Rollback != nil {
				if rollbackErr := configResult.Rollback(); rollbackErr != nil {
					return fmt.Errorf("保存代理 API 失败: %v；外部配置回退失败: %w", err, rollbackErr)
				}
			}
			return err
		}
		return nil
	}()
	if err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
}

// applyProfileInput applies the normalized editor input to a supplied config
// snapshot and returns the previous and resulting Profile for transaction checks.
func applyProfileInput(cfg *config.AppConfig, input *ProfileInput, modelsOmitted bool) (config.Profile, config.Profile, bool, error) {
	index := config.FindProfileIndex(cfg.Profiles, input.ID)
	var previous config.Profile
	if index >= 0 {
		previous = cfg.Profiles[index]
		if input.Models == nil {
			input.Models = append([]ModelInput(nil), configModelsToInput(previous.Models)...)
		}
		if modelsOmitted && input.DefaultModel == "" && previous.DefaultModel != "" {
			input.DefaultModel = previous.DefaultModel
		}
		// Profile 状态快照不会下发完整 API 密钥。编辑已有 Profile 时，
		// 前端留空代表“保持原密钥”，避免脱敏字段或空值覆盖本地密钥。
		if input.APIKey == "" {
			input.APIKey = previous.APIKey
		}
	}
	if index >= 0 && previous.Source == config.SourceDoge {
		if input.Source != config.SourceDoge {
			return config.Profile{}, config.Profile{}, false, errors.New("二狗子代理 API 来源不能修改")
		}
		// 二狗子密钥由远端令牌接口维护；后端再次校验，避免绕过前端只读控件修改。
		if normalizeDogeAPIKey(input.APIKey) != normalizeDogeAPIKey(previous.APIKey) {
			return config.Profile{}, config.Profile{}, false, errors.New("二狗子 API 密钥由远端管理，不能修改")
		}
		input.APIKey = normalizeDogeAPIKey(previous.APIKey)
		if input.DogeGroup != "" {
			if !dogeContainsString(cfg.Doge.Groups, input.DogeGroup) {
				return config.Profile{}, config.Profile{}, false, errors.New("所选远端分组当前不可用，请先刷新目录")
			}
			foundToken := false
			for tokenIndex := range cfg.Doge.Tokens {
				if cfg.Doge.Tokens[tokenIndex].ID != previous.RemoteTokenID {
					continue
				}
				foundToken = true
				break
			}
			if !foundToken {
				return config.Profile{}, config.Profile{}, false, errors.New("二狗子令牌不存在，请先刷新目录")
			}
		}
	} else if input.DogeGroup != "" {
		return config.Profile{}, config.Profile{}, false, errors.New("只有已导入的二狗子令牌可以修改远端分组")
	}
	profile := config.Profile{
		ID: input.ID, Source: input.Source, Category: input.Category, Name: input.Name, BaseURL: input.BaseURL,
		APIKey: input.APIKey, Note: input.Note, Headers: input.Headers, Models: modelInputsToConfig(input.Models), DefaultModel: input.DefaultModel,
	}
	if index >= 0 {
		profile.RemoteTokenID = previous.RemoteTokenID
		profile.SkipAutoSwitch = previous.SkipAutoSwitch
	} else {
		profile.ID = config.NewProfileID()
	}
	if profile.APIKey == "" {
		return config.Profile{}, config.Profile{}, false, errors.New("API 密钥不能为空")
	}
	if len([]rune(profile.Note)) > 160 {
		return config.Profile{}, config.Profile{}, false, errors.New("备注说明不能超过 160 个字符")
	}
	if err := config.ValidateProfile(profile); err != nil {
		return config.Profile{}, config.Profile{}, false, err
	}
	if index >= 0 {
		cfg.Profiles[index] = profile
		if profile.Source == config.SourceDoge && profile.RemoteTokenID > 0 {
			for tokenIndex := range cfg.Doge.Tokens {
				if cfg.Doge.Tokens[tokenIndex].ID == profile.RemoteTokenID {
					cfg.Doge.Tokens[tokenIndex].Category = profile.Category
					break
				}
			}
		}
		if previous.Category != profile.Category && cfg.ActiveProfiles[previous.Category] == profile.ID {
			delete(cfg.ActiveProfiles, previous.Category)
		}
	} else {
		cfg.Profiles = append(cfg.Profiles, profile)
	}
	cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
	return previous, profile, index >= 0, nil
}

// sameClientRenderedProfile compares every Profile field consumed by the
// external client renderers. Other Profile metadata does not affect those files.
func sameClientRenderedProfile(expected, current config.Profile) bool {
	return expected.ID == current.ID &&
		expected.Category == current.Category &&
		expected.DefaultModel == current.DefaultModel &&
		sameModelCatalog(expected.Models, current.Models)
}

func sameProfileForSave(expected, current config.Profile) bool {
	return expected.ID == current.ID && expected.Source == current.Source && expected.Category == current.Category &&
		expected.Name == current.Name && expected.BaseURL == current.BaseURL && expected.APIKey == current.APIKey &&
		expected.Note == current.Note && maps.Equal(expected.Headers, current.Headers) && sameModelCatalog(expected.Models, current.Models) &&
		expected.DefaultModel == current.DefaultModel && expected.RemoteTokenID == current.RemoteTokenID &&
		expected.SkipAutoSwitch == current.SkipAutoSwitch
}

func sameModelCatalog(expected, current []config.ModelEntry) bool {
	return (expected == nil) == (current == nil) && slices.Equal(expected, current)
}

// SetProfileAutoSwitch 设置单个 Profile 是否参加自动故障切换；手动提示模式仍可选择该 Profile。
func (s *DesktopService) SetProfileAutoSwitch(id string, enabled bool) error {
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, strings.TrimSpace(id))
		if index < 0 {
			return errors.New("代理 API 不存在")
		}
		cfg.Profiles[index].SkipAutoSwitch = !enabled
		return nil
	}); err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
}

func (s *DesktopService) DeleteProfile(id string) error {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	return s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, id)
		if index < 0 {
			return errors.New("代理 API 不存在")
		}
		category := cfg.Profiles[index].Category
		cfg.Profiles = append(cfg.Profiles[:index], cfg.Profiles[index+1:]...)
		if cfg.ActiveProfiles[category] == id {
			delete(cfg.ActiveProfiles, category)
		}
		cfg.FailoverOrder = config.NormalizeFailoverOrder(cfg.FailoverOrder, cfg.Profiles)
		return nil
	})
}

// ActivateProfile 启用指定 Profile。第二个参数用于明确控制是否同步外部
// 客户端配置：桌面端确认配置时传 true，用户跳过或兼容旧调用时传 false。
// 外部文件提交成功后才保存 ActiveProfiles；保存失败会恢复外部文件。
func (s *DesktopService) ActivateProfile(id string, configure ...bool) error {
	applyClientConfig := len(configure) > 0 && configure[0]
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	return s.activateProfile(id, applyClientConfig)
}

// activateProfileFromTray 只同步已经由 CodexRelay 接管的客户端。托盘点击是
// Profile 切换操作，不等同于用户确认覆盖一个尚未接管的外部配置。
func (s *DesktopService) activateProfileFromTray(id string) error {
	s.clientConfigMu.Lock()
	defer s.clientConfigMu.Unlock()
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	index := config.FindProfileIndex(state.Config.Profiles, id)
	if index < 0 {
		return errors.New("代理 API 不存在")
	}
	category := state.Config.Profiles[index].Category
	entry := state.Config.ClientConfigs[category]
	applyClientConfig := false
	if clientconfig.Supports(category) && !entry.SkipConfigReplacement {
		status, err := clientconfig.Inspect(state.Config, category)
		if err != nil {
			return fmt.Errorf("检查客户端配置失败: %w", err)
		}
		if status.Status == "error" {
			return fmt.Errorf("检查客户端配置失败: %s", status.Error)
		}
		applyClientConfig = status.Configured
	}
	return s.activateProfile(id, applyClientConfig)
}

func (s *DesktopService) activateProfile(id string, applyClientConfig bool) error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("程序尚未初始化")
	}
	previousID := ""
	index := config.FindProfileIndex(state.Config.Profiles, id)
	if index < 0 {
		return errors.New("代理 API 不存在")
	}
	candidate := state.Config.Profiles[index]
	category := candidate.Category
	previousID = state.Config.ActiveProfiles[category]
	var configResult clientconfig.ConfigureResult
	clientConfigRendered := false
	var err error
	if applyClientConfig && clientconfig.Supports(category) && !state.Config.ClientConfigs[category].SkipConfigReplacement {
		configResult, err = clientconfig.ConfigureWithResult(state.Config, category, id)
		if err != nil {
			return fmt.Errorf("更新客户端配置失败: %w", err)
		}
		clientConfigRendered = true
	}
	err = s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, id)
		if index >= 0 {
			profile := cfg.Profiles[index]
			if clientConfigRendered && !sameClientRenderedProfile(candidate, profile) {
				return errors.New("代理 API 的客户端配置字段已被并发修改，请重试")
			}
			if profile.Source == config.SourceDoge {
				if profile.RemoteTokenID <= 0 {
					return errors.New("二狗子令牌缺少有效的远端目录 ID，不能启用")
				}
				found := false
				for _, token := range cfg.Doge.Tokens {
					if token.ID != profile.RemoteTokenID {
						continue
					}
					found = true
					if !dogeTokenAvailable(token, cfg.Doge.Groups) {
						return errors.New("二狗子令牌当前分组不可用，不能启用")
					}
					break
				}
				if !found {
					return errors.New("二狗子令牌已不在最新目录中，请先同步")
				}
			}
			if cfg.ActiveProfiles == nil {
				cfg.ActiveProfiles = map[string]string{}
			}
			cfg.ActiveProfiles[profile.Category] = id
			return nil
		}
		return errors.New("代理 API 不存在")
	})
	if err != nil {
		if configResult.Rollback != nil {
			if rollbackErr := configResult.Rollback(); rollbackErr != nil {
				return fmt.Errorf("启用代理 API 失败: %v；外部配置回退失败: %w", err, rollbackErr)
			}
		}
		return err
	}
	if previousID != "" && previousID != id {
		s.runtime.ResetProfileHealth(previousID)
	}
	return nil
}
