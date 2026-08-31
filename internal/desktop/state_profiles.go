package desktop

import (
	"net/url"
	"strconv"
	"strings"

	"codexrelay/internal/config"
	"codexrelay/internal/relay"
)

func publicProfile(profile config.Profile, activeProfiles map[string]string) PublicProfile {
	u, _ := url.Parse(profile.BaseURL)
	preview := ""
	if u != nil {
		u.Path = relay.JoinTargetPath(u.Path, "/v1/responses")
		preview = u.String()
	}
	return PublicProfile{
		ID: profile.ID, Source: profile.Source, Category: profile.Category, Name: profile.Name, BaseURL: profile.BaseURL,
		APIKey: "", APIKeyConfigured: strings.TrimSpace(profile.APIKey) != "", APIKeyHint: maskAPIKey(profile.APIKey),
		Note: profile.Note, Headers: profile.Headers, Models: publicModels(profile.Models), DefaultModel: profile.DefaultModel,
		Active: activeProfiles[profile.Category] == profile.ID, PreviewURL: preview, RemoteTokenID: profile.RemoteTokenID, SkipAutoSwitch: profile.SkipAutoSwitch,
	}
}

// maskAPIKey 只返回不可逆的展示提示。完整供应商密钥不会出现在桌面状态
// 快照或 Wails 返回值中；长度较短的密钥也不直接暴露原文。
func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	runes := []rune(key)
	if len(runes) <= 16 {
		return strings.Repeat("*", len(runes))
	}
	return "********" + string(runes[len(runes)-4:])
}

func maskDogeKey(key string) string {
	return maskAPIKey(key)
}

// dogeTokenNote 为首次同步的令牌生成可读备注；只使用接口返回的掩码密钥和额度摘要，不写入完整密钥。
func dogeTokenNote(token config.DogeToken) string {
	key := strings.TrimSpace(token.MaskedKey)
	if key == "" {
		key = maskDogeKey(token.Key)
	}
	if key == "" {
		return ""
	}
	quota := "不限额度"
	if !token.UnlimitedQuota {
		quota = "剩余 " + strconv.FormatInt(token.RemainQuota, 10)
	}
	return normalizeDogeAPIKey(key) + " · " + quota
}
