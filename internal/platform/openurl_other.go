//go:build !windows

/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 非 Windows 默认浏览器打开外部链接
 * @File          : 非 Windows 外部 URL 打开实现
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package platform

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// OpenURL 将受限的 HTTP(S) 地址交给当前桌面环境默认浏览器；调用不会等待浏览器退出。
func OpenURL(raw string) error {
	parsed, err := validateExternalURL(raw)
	if err != nil {
		return err
	}
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	if err := exec.Command(command, parsed).Start(); err != nil {
		return errors.New("无法启动默认浏览器")
	}
	return nil
}

func validateExternalURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("外部链接地址无效")
	}
	return parsed.String(), nil
}
