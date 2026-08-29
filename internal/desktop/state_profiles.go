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
		APIKey: profile.APIKey, Note: profile.Note, Headers: profile.Headers, Models: publicModels(profile.Models), DefaultModel: profile.DefaultModel,
		Active: activeProfiles[profile.Category] == profile.ID, PreviewURL: preview, RemoteTokenID: profile.RemoteTokenID, SkipAutoSwitch: profile.SkipAutoSwitch,
	}
}

func maskDogeKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "**********" + key[len(key)-4:]
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
