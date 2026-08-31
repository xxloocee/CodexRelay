package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"codexrelay/internal/config"
)

func sanitizeDogeUser(user map[string]any) map[string]any {
	result := make(map[string]any)
	for _, key := range []string{"id", "user_id", "username", "display_name", "displayName", "email", "role", "group", "status"} {
		if value, ok := user[key]; ok {
			result[key] = value
		}
	}
	return result
}

func (s *DesktopService) fetchDogeGroups(ctx context.Context, client *http.Client, baseURL, accessToken string) ([]string, map[string]dogeGroupInfo, error) {
	envelope, err := s.dogeRequestEnvelopeWithClient(ctx, client, baseURL, accessToken, http.MethodGet, "/api/user/self/groups")
	if err != nil {
		return nil, nil, err
	}
	var raw any
	if err := json.Unmarshal(envelope.Data, &raw); err != nil {
		return nil, nil, fmt.Errorf("二狗子分组信息格式无效: %w", err)
	}
	groups, details := parseDogeGroups(raw)
	return orderDogeGroups(groups, envelope.GroupOrder), details, nil
}

func orderDogeGroups(groups, preferred []string) []string {
	available := make(map[string]struct{}, len(groups))
	for _, group := range uniqueStrings(groups) {
		available[group] = struct{}{}
	}
	ordered := make([]string, 0, len(available))
	for _, group := range uniqueStrings(preferred) {
		if _, ok := available[group]; !ok {
			continue
		}
		ordered = append(ordered, group)
		delete(available, group)
	}
	remaining := make([]string, 0, len(available))
	for group := range available {
		remaining = append(remaining, group)
	}
	sort.Strings(remaining)
	return append(ordered, remaining...)
}

func dogeGroupDisplayNames(groups []string, details map[string]dogeGroupInfo) map[string]string {
	result := make(map[string]string, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(details[group].DisplayName)
		if name == "" {
			name = group
		}
		result[group] = name
	}
	return result
}

// parseDogeGroups 解析分组键与展示元数据；分组键用于匹配令牌，展示名和倍率只用于界面。
func parseDogeGroups(value any) ([]string, map[string]dogeGroupInfo) {
	if item, ok := value.(map[string]any); ok {
		groups := make([]string, 0, len(item))
		details := make(map[string]dogeGroupInfo, len(item))
		for key, child := range item {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			info := dogeGroupInfo{}
			if metadata, ok := child.(map[string]any); ok {
				if displayName, ok := metadata["display_name"].(string); ok && strings.TrimSpace(displayName) != "" {
					info.DisplayName = strings.TrimSpace(displayName)
				}
				if ratio, ok := metadata["ratio"].(float64); ok {
					info.Ratio = ratio
				}
			}
			// 权限目录保存接口对象键；该键不参与用户界面文案。
			groups = append(groups, key)
			details[key] = info
		}
		if len(details) > 0 {
			return uniqueStrings(groups), details
		}
	}
	return uniqueStrings(collectDogeGroups(value)), map[string]dogeGroupInfo{}
}

func collectDogeGroups(value any) []string {
	switch item := value.(type) {
	case string:
		if strings.TrimSpace(item) != "" {
			return []string{strings.TrimSpace(item)}
		}
	case []any:
		var result []string
		for _, child := range item {
			result = append(result, collectDogeGroups(child)...)
		}
		return result
	case map[string]any:
		var result []string
		known := map[string]bool{"data": true, "groups": true, "items": true, "list": true, "name": true}
		for _, key := range []string{"data", "groups", "items", "list"} {
			if child, ok := item[key]; ok {
				result = append(result, collectDogeGroups(child)...)
			}
		}
		if name, ok := item["name"].(string); ok && len(result) == 0 {
			result = append(result, strings.TrimSpace(name))
		}
		if len(result) == 0 {
			for key := range item {
				if !known[key] && strings.TrimSpace(key) != "" {
					result = append(result, key)
				}
			}
		}
		return result
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// dogeTokenPermitted 按最近一次同步的分组目录判断令牌所属分组是否有权限；目录可能保存展示名或原始分组键。
func dogeTokenPermitted(token config.DogeToken, groups []string) bool {
	group := strings.TrimSpace(token.Group)
	displayName := strings.TrimSpace(token.GroupDisplayName)
	if group == "" && displayName == "" {
		return false
	}
	for _, available := range groups {
		available = strings.TrimSpace(available)
		if available != "" && (available == group || available == displayName) {
			return true
		}
	}
	return false
}

// dogeTokenDisplayGroup 返回令牌在用户界面中的分组文案。
// 分组目录中的原始键只用于权限判断；用户可见名称必须来自同步得到的 display_name，缺失时保持为空。
func dogeTokenDisplayGroup(token config.DogeToken) string {
	return strings.TrimSpace(token.GroupDisplayName)
}

// dogeTokenAvailable 在目录状态和用户分组两个维度判断令牌是否可选择。
// 当前上游样本中 status=1 表示正常令牌；其他状态不进入启用或切换候选。
func dogeTokenAvailable(token config.DogeToken, groups []string) bool {
	return token.Status == 1 && dogeTokenPermitted(token, groups)
}

// dogeTokenSwitchable 判断令牌是否满足主窗口、托盘和切换服务共同使用的可切换约束。
// 完整本地密钥只从配置读取，掩码密钥不能进入启用或切换入口。
func dogeTokenSwitchable(token config.DogeToken, groups []string) bool {
	return dogeTokenAvailable(token, groups) && isCompleteDogeAPIKey(token.Key)
}

// availableDogeTokensForCategory 返回主窗口该类别下可实际切换的二狗子令牌。
// 类别为空时沿用已导入 Profile 的类别；状态、权限和完整本地密钥沿用主窗口可用令牌的后端约束。
// 该集合由托盘菜单、切换提示和切换服务端复核共同使用，不按远端分组额外缩小范围。
func availableDogeTokensForCategory(tokens []config.DogeToken, profilesByRemoteID map[int64]config.Profile, groups []string, category string) []config.DogeToken {
	category = strings.TrimSpace(category)
	if category == "" {
		return nil
	}
	available := make([]config.DogeToken, 0, len(tokens))
	for _, token := range tokens {
		if token.ID <= 0 {
			continue
		}
		resolvedCategory := strings.TrimSpace(token.Category)
		if resolvedCategory == "" {
			if profile, ok := profilesByRemoteID[token.ID]; ok {
				resolvedCategory = strings.TrimSpace(profile.Category)
			}
		}
		if resolvedCategory != category || !dogeTokenSwitchable(token, groups) {
			continue
		}
		token.Category = resolvedCategory
		available = append(available, token)
	}
	return available
}

func (s *DesktopService) fetchDogeTokens(ctx context.Context, client *http.Client, baseURL, accessToken string) ([]config.DogeToken, error) {
	const pageSize = 100
	all := make([]config.DogeToken, 0)
	for page := 1; page <= 100; page++ {
		data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, fmt.Sprintf("/api/token/?p=%d&page_size=%d", page, pageSize))
		if err != nil {
			return nil, err
		}
		var payload dogeTokenPage
		if err := json.Unmarshal(data, &payload); err != nil {
			var direct []dogeTokenResponse
			if directErr := json.Unmarshal(data, &direct); directErr != nil {
				return nil, fmt.Errorf("二狗子令牌列表格式无效: %w", err)
			}
			payload.Items = direct
		}
		for _, item := range payload.Items {
			all = append(all, dogeTokenFromResponse(item))
		}
		if len(payload.Items) == 0 || (payload.Total > 0 && len(all) >= payload.Total) || len(payload.Items) < pageSize {
			break
		}
	}
	return all, nil
}

// fetchDogeToken 读取单个令牌的实时可更新字段。分组编辑必须使用这份快照，
// 避免把本地目录中可能过期的额度、有效期或限制设置回写到远端。
func (s *DesktopService) fetchDogeToken(ctx context.Context, client *http.Client, baseURL, accessToken string, id int64) (config.DogeToken, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, fmt.Sprintf("/api/token/%d", id))
	if err != nil {
		return config.DogeToken{}, err
	}
	var payload dogeTokenResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return config.DogeToken{}, fmt.Errorf("二狗子令牌详情格式无效: %w", err)
	}
	if payload.ID != id {
		return config.DogeToken{}, errors.New("二狗子令牌详情与请求不匹配")
	}
	return dogeTokenFromResponse(payload), nil
}

func dogeTokenFromResponse(item dogeTokenResponse) config.DogeToken {
	return config.DogeToken{
		ID: item.ID, UserID: item.UserID, MaskedKey: item.Key,
		Status: item.Status, Name: item.Name, CreatedTime: item.CreatedTime,
		AccessedTime: item.AccessedTime, ExpiredTime: item.ExpiredTime,
		RemainQuota: item.RemainQuota, UnlimitedQuota: item.UnlimitedQuota,
		ModelLimitsEnabled: item.ModelLimitsEnabled, ModelLimits: item.ModelLimits,
		AllowIPs: item.AllowIPs, UsedQuota: item.UsedQuota, Group: item.Group,
		CrossGroupRetry: item.CrossGroupRetry,
	}
}

func (s *DesktopService) fetchDogeTokenKey(ctx context.Context, client *http.Client, baseURL, accessToken string, id int64) (string, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodPost, fmt.Sprintf("/api/token/%d/key", id))
	if err != nil {
		return "", err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("二狗子令牌密钥格式无效: %w", err)
	}
	key := findDogeKey(raw)
	if key == "" {
		return "", errors.New("二狗子接口没有返回完整 API 密钥")
	}
	return normalizeDogeAPIKey(key), nil
}

// normalizeDogeAPIKey 统一二狗子接口返回的令牌格式，避免同一个令牌出现两种前缀。
func normalizeDogeAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "sk-") {
		return key
	}
	return "sk-" + key
}

// isCompleteDogeAPIKey 识别可用于代理认证的本地密钥；二狗子令牌列表的掩码值包含星号，不能直接复用。
func isCompleteDogeAPIKey(key string) bool {
	key = strings.TrimSpace(key)
	return key != "" && !strings.Contains(key, "*")
}

func findDogeKey(value any) string {
	switch item := value.(type) {
	case string:
		return strings.TrimSpace(item)
	case map[string]any:
		if key, ok := item["key"]; ok {
			if value := findDogeKey(key); value != "" {
				return value
			}
		}
		if data, ok := item["data"]; ok {
			return findDogeKey(data)
		}
	}
	return ""
}
