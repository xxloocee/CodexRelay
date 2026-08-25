/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 配置字段与中转字段校验
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"codexrelay/internal/network"
)

func Validate(cfg AppConfig) error {
	if err := ValidatePreferences(cfg.Preferences); err != nil {
		return err
	}
	if err := ValidateTokenSwitch(cfg.TokenSwitch); err != nil {
		return err
	}
	if err := ValidateTaskNotification(cfg.TaskNotification); err != nil {
		return err
	}
	if !strings.HasPrefix(cfg.LocalAccessToken, "sk-") {
		return errors.New("本地访问令牌必须以 sk- 开头")
	}
	if cfg.Profiles == nil {
		return errors.New("profiles 必须是 JSON 数组")
	}
	if cfg.ActiveProfiles == nil {
		return errors.New("activeProfiles 必须是 JSON 对象")
	}
	if cfg.ClientConfigs == nil {
		return errors.New("clientConfigs 必须是 JSON 对象")
	}
	if err := network.Validate(cfg.Network, cfg.ProxyPort); err != nil {
		return err
	}
	if err := ValidateDoge(cfg.Doge); err != nil {
		return err
	}
	profileIDs := make(map[string]Profile, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		if strings.TrimSpace(profile.APIKey) == "" {
			return fmt.Errorf("代理 API %q 的 API 密钥不能为空", profile.Name)
		}
		if len([]rune(profile.Note)) > 160 {
			return fmt.Errorf("代理 API %q 的备注说明不能超过 160 个字符", profile.Name)
		}
		if err := ValidateProfile(profile); err != nil {
			return fmt.Errorf("代理 API %q 配置无效: %w", profile.Name, err)
		}
		if _, exists := profileIDs[profile.ID]; exists {
			return fmt.Errorf("代理 API ID %q 重复", profile.ID)
		}
		profileIDs[profile.ID] = profile
	}
	for category, id := range cfg.ActiveProfiles {
		if !IsCategory(category) {
			return fmt.Errorf("启用映射包含未知 API 类别 %q", category)
		}
		profile, ok := profileIDs[id]
		if !ok {
			return fmt.Errorf("类别 %q 启用的代理 API 不存在", category)
		}
		if profile.Category != category {
			return fmt.Errorf("类别 %q 的启用代理 API 类别不匹配", category)
		}
	}
	for category := range cfg.ClientConfigs {
		if !IsCategory(category) {
			return fmt.Errorf("客户端配置包含未知 API 类别 %q", category)
		}
	}
	if err := ValidateFailoverOrder(cfg.FailoverOrder, cfg.Profiles); err != nil {
		return err
	}
	return nil
}

// ValidateTokenSwitch 校验通用故障切换模式、触发阈值和时间窗口。
func ValidateTokenSwitch(settings TokenSwitchSettings) error {
	if settings.Mode != TokenSwitchModePrompt && settings.Mode != TokenSwitchModeAuto {
		return fmt.Errorf("令牌异常处理方式无效: %q", settings.Mode)
	}
	if settings.AuthFailureThreshold < 1 {
		return errors.New("401/403 连续失败次数必须大于 0")
	}
	if settings.UpstreamFailureThreshold < 1 {
		return errors.New("5XX/连接异常累计次数必须大于 0")
	}
	if settings.UpstreamFailureWindowMinutes < 1 {
		return errors.New("5XX/连接异常统计窗口必须大于 0 分钟")
	}
	return nil
}

// ValidateTaskNotification 校验本机 watcher 的直接访问 URL。地址由用户完整提供，
// 当前实现只允许 HTTP(S)，且不允许把认证材料嵌入 URL userinfo。
func ValidateTaskNotification(notification TaskNotification) error {
	notification = NormalizeTaskNotification(notification)
	if notification.IdleGraceSeconds < 1 || notification.IdleGraceSeconds > 60 {
		return errors.New("任务通知静默时间必须是 1 到 60 秒之间的整数")
	}
	if notification.RequestTimeoutSeconds < 1 || notification.RequestTimeoutSeconds > 60 {
		return errors.New("任务通知请求超时必须是 1 到 60 秒之间的整数")
	}
	if notification.MaxAttempts < 0 || notification.MaxAttempts > 1000 {
		return errors.New("任务通知最大重试次数必须是 0 到 1000 之间的整数")
	}
	endpoint := strings.TrimSpace(notification.WebhookURL)
	if !notification.Enabled && endpoint == "" {
		return nil
	}
	parseEndpoint := strings.NewReplacer("{title}", "title-placeholder", "{content}", "content-placeholder").Replace(endpoint)
	u, err := url.Parse(parseEndpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("任务通知 Webhook 地址必须是有效的 http:// 或 https:// 地址")
	}
	if endpoint != notification.WebhookURL {
		return errors.New("任务通知 Webhook 地址不能包含首尾空白字符")
	}
	if notification.Enabled && endpoint == "" {
		return errors.New("开启任务通知前请填写 Webhook 地址")
	}
	return nil
}

// ValidateFailoverOrder 校验按类别保存的 Profile 顺序；顺序只引用同类别的本地 Profile。
func ValidateFailoverOrder(order map[string][]string, profiles []Profile) error {
	if order == nil {
		return errors.New("failoverOrder 必须是 JSON 对象")
	}
	byID := make(map[string]Profile, len(profiles))
	for _, profile := range profiles {
		byID[profile.ID] = profile
	}
	for category, ids := range order {
		if !IsCategory(category) {
			return fmt.Errorf("令牌切换顺序包含未知类别 %q", category)
		}
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
				return fmt.Errorf("类别 %q 的令牌切换顺序包含无效 Profile ID", category)
			}
			if _, ok := seen[id]; ok {
				return fmt.Errorf("类别 %q 的令牌切换顺序包含重复 Profile ID %q", category, id)
			}
			profile, ok := byID[id]
			if !ok || profile.Category != category {
				return fmt.Errorf("类别 %q 的令牌切换顺序包含未知或跨类别 Profile %q", category, id)
			}
			seen[id] = struct{}{}
		}
	}
	return nil
}

// ValidatePreferences 校验主页展示和窗口恢复偏好；这些字段只影响界面状态，不改变代理路由或启用映射。
func ValidatePreferences(preferences Preferences) error {
	if len(preferences.VisibleCategories) == 0 {
		return errors.New("主页至少需要显示一个 API 类别")
	}
	seen := make(map[string]struct{}, len(preferences.VisibleCategories))
	for _, rawCategory := range preferences.VisibleCategories {
		category := strings.TrimSpace(rawCategory)
		if category != rawCategory {
			return fmt.Errorf("主页显示类别无效: %q", rawCategory)
		}
		if !IsCategory(category) {
			return fmt.Errorf("主页显示类别无效: %q", category)
		}
		if _, ok := seen[category]; ok {
			return fmt.Errorf("主页显示类别重复: %q", category)
		}
		seen[category] = struct{}{}
	}
	if strings.TrimSpace(preferences.DefaultSource) != preferences.DefaultSource {
		return fmt.Errorf("主程序默认来源无效: %q", preferences.DefaultSource)
	}
	if preferences.DefaultSource != "" && preferences.DefaultSource != SourceDoge && preferences.DefaultSource != SourceCustom {
		return fmt.Errorf("主程序默认来源无效: %q", preferences.DefaultSource)
	}
	if preferences.DefaultCategory != "" {
		if strings.TrimSpace(preferences.DefaultCategory) != preferences.DefaultCategory {
			return fmt.Errorf("主程序默认类别无效: %q", preferences.DefaultCategory)
		}
		if !IsCategory(preferences.DefaultCategory) {
			return fmt.Errorf("主程序默认类别无效: %q", preferences.DefaultCategory)
		}
		if _, ok := seen[preferences.DefaultCategory]; !ok {
			return fmt.Errorf("主程序默认类别必须处于主页显示状态: %q", preferences.DefaultCategory)
		}
	}
	if preferences.RestoreViewMode != RestoreViewCurrent && preferences.RestoreViewMode != RestoreViewDefault {
		return fmt.Errorf("恢复窗口显示模式无效: %q", preferences.RestoreViewMode)
	}
	return nil
}

func ValidateDoge(connection DogeConnection) error {
	if strings.TrimSpace(connection.BaseURL) == "" {
		return errors.New("二狗子 API 地址不能为空")
	}
	u, err := url.Parse(strings.TrimSpace(connection.BaseURL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("二狗子 API 地址必须是有效的 http:// 或 https:// 地址")
	}
	if !IsDogeSyncInterval(connection.SyncIntervalMinutes) {
		return errors.New("二狗子同步间隔必须是 1、3、5、10、15、30 或 60 分钟")
	}
	if connection.Notifications.BalanceAlertThresholdUSD <= 0 || connection.Notifications.SubscriptionAlertThresholdUSD <= 0 {
		return errors.New("余额和套餐提醒阈值必须大于 0")
	}
	if connection.Groups == nil || connection.Tokens == nil {
		return errors.New("二狗子分组和令牌目录必须是 JSON 数组")
	}
	tokenIDs := make(map[int64]struct{}, len(connection.Tokens))
	for _, token := range connection.Tokens {
		if token.ID <= 0 {
			return errors.New("二狗子令牌 ID 无效")
		}
		if _, exists := tokenIDs[token.ID]; exists {
			return fmt.Errorf("二狗子令牌 ID %d 重复", token.ID)
		}
		if token.Category != "" && !IsCategory(token.Category) {
			return fmt.Errorf("二狗子令牌 %d 的存放类别无效", token.ID)
		}
		tokenIDs[token.ID] = struct{}{}
	}
	orderKeys := make(map[string]struct{}, len(connection.TokenOrder))
	for _, key := range connection.TokenOrder {
		key = strings.TrimSpace(key)
		if key == "" {
			return errors.New("二狗子令牌排序 ID 不能为空")
		}
		if _, exists := orderKeys[key]; exists {
			return fmt.Errorf("二狗子令牌排序 ID %q 重复", key)
		}
		orderKeys[key] = struct{}{}
	}
	return nil
}

func ValidateProfile(profile Profile) error {
	if profile.Source != SourceDoge && profile.Source != SourceCustom {
		return errors.New("代理 API 来源必须是 doge 或 custom")
	}
	if !IsCategory(profile.Category) {
		return errors.New("代理 API 类别无效")
	}
	name := strings.TrimSpace(profile.Name)
	baseURL := strings.TrimSpace(profile.BaseURL)
	if name == "" {
		return errors.New("代理 API 名称不能为空")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("API 地址必须是有效的 http:// 或 https:// 地址")
	}
	if u.User != nil {
		return errors.New("API 地址不能包含用户名或密码")
	}
	for headerName, value := range profile.Headers {
		if strings.TrimSpace(headerName) == "" || strings.ContainsAny(headerName, "\r\n") {
			return errors.New("额外请求头名称无效")
		}
		if strings.EqualFold(headerName, "Authorization") || strings.EqualFold(headerName, "Host") {
			return fmt.Errorf("%s 由代理管理，不能作为额外请求头", headerName)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("请求头 %s 的值不能包含换行", headerName)
		}
	}
	if err := ValidateModels(profile.Models, profile.DefaultModel); err != nil {
		return err
	}
	return nil
}

// ValidateModels 校验本地模型目录的身份、去重和默认项关系；模型 ID 必须由用户或上游真实返回。
func ValidateModels(models []ModelEntry, defaultModel string) error {
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" || id != model.ID {
			return errors.New("模型 ID 不能为空或包含首尾空白")
		}
		if strings.ContainsAny(id, "\r\n") {
			return fmt.Errorf("模型 %q 的 ID 不能包含换行", id)
		}
		if model.ContextWindow < 0 {
			return fmt.Errorf("模型 %q 的上下文窗口不能为负数", id)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("模型 ID %q 重复", id)
		}
		seen[id] = struct{}{}
	}
	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel != "" {
		if _, ok := seen[defaultModel]; !ok {
			return fmt.Errorf("默认模型 %q 不在模型目录中", defaultModel)
		}
	}
	return nil
}

func IsCategory(category string) bool {
	for _, item := range Categories {
		if item == category {
			return true
		}
	}
	return false
}
