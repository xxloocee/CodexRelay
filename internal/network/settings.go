/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 出站网络设置模型与校验
 */
package network

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type Settings struct {
	Mode     string `json:"mode"`
	ProxyURL string `json:"proxyUrl,omitempty"`
}

type SystemProxyInfo struct {
	Enabled    bool   `json:"enabled"`
	HTTPProxy  string `json:"httpProxy,omitempty"`
	HTTPSProxy string `json:"httpsProxy,omitempty"`
	Bypass     string `json:"bypass,omitempty"`
	Source     string `json:"source"`
	Note       string `json:"note,omitempty"`
	PACURL     string `json:"-"`
}

func Validate(settings Settings, listenerPort int) error {
	switch settings.Mode {
	case "system", "direct":
		return nil
	case "manual":
		u, err := url.Parse(strings.TrimSpace(settings.ProxyURL))
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("手动代理必须是有效的 http:// 或 https:// 地址")
		}
		if IsListenerURL(u, listenerPort) {
			return errors.New("上游网络代理不能指向 CodexRelay 自己，否则会形成循环")
		}
		return nil
	default:
		return errors.New("未知网络出口模式")
	}
}

func IsListenerURL(u *url.URL, listenerPort int) bool {
	host := strings.ToLower(u.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return false
	}
	port, _ := strconv.Atoi(u.Port())
	return port == listenerPort
}
