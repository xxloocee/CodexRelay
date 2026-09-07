//go:build windows

/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : Windows 系统代理只读检测
 */
package network

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func DetectSystemProxy() SystemProxyInfo {
	info := SystemProxyInfo{Source: "Windows 系统代理"}
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		info.Note = "无法读取 Windows 系统代理，出站将使用系统路由"
		return info
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		if pac, _, pacErr := key.GetStringValue("AutoConfigURL"); pacErr == nil && strings.TrimSpace(pac) != "" {
			info.PACURL = strings.TrimSpace(pac)
			info.Note = "检测到 PAC 自动脚本；当前版本不解析 PAC，VPN/TUN 路由仍然有效"
		} else {
			info.Note = "系统 HTTP 代理未启用；VPN/TUN 路由仍然有效"
		}
		return info
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		info.Note = "系统代理已启用，但未读取到代理地址"
		return info
	}
	bypass, _, _ := key.GetStringValue("ProxyOverride")
	info.Bypass = bypass
	info.HTTPProxy, info.HTTPSProxy = parseWindowsProxyServer(server)
	info.Enabled = info.HTTPProxy != "" || info.HTTPSProxy != ""
	if info.Enabled {
		info.Note = "CodexRelay 的上游请求会继续经过该代理"
	}
	return info
}

func parseWindowsProxyServer(server string) (string, string) {
	server = strings.TrimSpace(server)
	if !strings.Contains(server, "=") {
		value := normalizeProxyAddress(server)
		return value, value
	}
	values := map[string]string{}
	for _, item := range strings.Split(server, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		if ok {
			values[strings.ToLower(strings.TrimSpace(name))] = normalizeProxyAddress(value)
		}
	}
	httpProxy := values["http"]
	httpsProxy := values["https"]
	if httpProxy == "" {
		httpProxy = httpsProxy
	}
	if httpsProxy == "" {
		httpsProxy = httpProxy
	}
	return httpProxy, httpsProxy
}

func normalizeProxyAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.String()
}

func systemProxyFunc(info SystemProxyInfo) func(*http.Request) (*url.URL, error) {
	return func(request *http.Request) (*url.URL, error) {
		requestURL := request.URL
		if !info.Enabled || shouldBypassWindowsProxy(requestURL.Hostname(), info.Bypass) {
			return nil, nil
		}
		proxyAddress := info.HTTPProxy
		if requestURL.Scheme == "https" {
			proxyAddress = info.HTTPSProxy
		}
		if proxyAddress == "" {
			return nil, nil
		}
		proxyURL, err := url.Parse(proxyAddress)
		if err != nil {
			return nil, fmt.Errorf("解析 Windows 系统代理失败: %w", err)
		}
		return proxyURL, nil
	}
}

func shouldBypassWindowsProxy(host, bypass string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	if host == "localhost" {
		return true
	}
	for _, entry := range strings.Split(bypass, ";") {
		entry = strings.TrimSpace(strings.ToLower(entry))
		switch {
		case entry == "":
			continue
		case entry == "<local>" && !strings.Contains(host, "."):
			return true
		case strings.HasPrefix(entry, "*.") && (host == entry[2:] || strings.HasSuffix(host, entry[1:])):
			return true
		case entry == host:
			return true
		}
	}
	return false
}
