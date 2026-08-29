package desktop

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"codexrelay/internal/config"
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
// 保存成功后重新评估正在进行的自动轮次，使新 Profile 可以成为目录异常后的实时候选。
func (s *DesktopService) SaveProfile(input ProfileInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.Note = strings.TrimSpace(input.Note)
	input.DefaultModel = strings.TrimSpace(input.DefaultModel)
	if input.Headers == nil {
		input.Headers = map[string]string{}
	}
	modelsOmitted := input.Models == nil
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, input.ID)
		var previous config.Profile
		if index >= 0 {
			previous = cfg.Profiles[index]
			if modelsOmitted {
				input.Models = append([]ModelInput(nil), configModelsToInput(previous.Models)...)
			}
			if modelsOmitted && input.DefaultModel == "" && previous.DefaultModel != "" {
				input.DefaultModel = previous.DefaultModel
			}
		}
		if index >= 0 && previous.Source == config.SourceDoge {
			if input.Source != config.SourceDoge {
				return errors.New("二狗子代理 API 来源不能修改")
			}
			// 二狗子密钥由远端令牌接口维护；后端再次校验，避免绕过前端只读控件修改。
			if normalizeDogeAPIKey(input.APIKey) != normalizeDogeAPIKey(previous.APIKey) {
				return errors.New("二狗子 API 密钥由远端管理，不能修改")
			}
			input.APIKey = normalizeDogeAPIKey(previous.APIKey)
		}
		profile := config.Profile{
			ID: input.ID, Source: input.Source, Category: input.Category, Name: input.Name, BaseURL: input.BaseURL,
			APIKey: input.APIKey, Note: input.Note, Headers: input.Headers, Models: modelInputsToConfig(input.Models), DefaultModel: input.DefaultModel,
		}
		if index >= 0 {
			profile.RemoteTokenID = previous.RemoteTokenID
			profile.SkipAutoSwitch = previous.SkipAutoSwitch
		}
		if index < 0 {
			profile.ID = config.NewProfileID()
		}
		if profile.APIKey == "" {
			return errors.New("API 密钥不能为空")
		}
		if len([]rune(profile.Note)) > 160 {
			return errors.New("备注说明不能超过 160 个字符")
		}
		if err := config.ValidateProfile(profile); err != nil {
			return err
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
		return nil
	}); err != nil {
		return err
	}
	s.handleHealthChanged()
	return nil
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

// ActivateProfile 启用指定 Profile；二狗子来源必须仍存在于最新目录且当前分组可用。
// 成功后清理原活动 Profile 的失败统计，新请求立即读取新的运行时快照。
func (s *DesktopService) ActivateProfile(id string) error {
	state := s.runtime.State()
	previousID := ""
	if state != nil {
		if index := config.FindProfileIndex(state.Config.Profiles, id); index >= 0 {
			previousID = state.Config.ActiveProfiles[state.Config.Profiles[index].Category]
		}
	}
	err := s.updateConfig(func(cfg *config.AppConfig) error {
		index := config.FindProfileIndex(cfg.Profiles, id)
		if index >= 0 {
			profile := cfg.Profiles[index]
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
		return err
	}
	if previousID != "" && previousID != id {
		s.runtime.ResetProfileHealth(previousID)
	}
	return nil
}
