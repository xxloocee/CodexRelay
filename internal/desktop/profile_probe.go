package desktop

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/network"
	"codexrelay/internal/relay"
)

func (s *DesktopService) TestProfile(id string) (TestResult, error) {
	state := s.runtime.State()
	index := config.FindProfileIndex(state.Config.Profiles, id)
	if index < 0 {
		return TestResult{}, errors.New("代理 API 不存在")
	}
	profile := config.CloneProfile(state.Config.Profiles[index])
	target, _ := url.Parse(profile.BaseURL)
	target.Path = relay.JoinTargetPath(target.Path, "/v1/models")
	transport, err := network.BuildTransport(state.Config.Network, network.DetectSystemProxy(), state.Config.ProxyPort)
	if err != nil {
		return TestResult{}, err
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	request, _ := http.NewRequest(http.MethodGet, target.String(), nil)
	request.Header.Set("Authorization", "Bearer "+profile.APIKey)
	for name, value := range profile.Headers {
		request.Header.Set(name, value)
	}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return TestResult{}, fmt.Errorf("连接失败: %s", relay.SanitizeError(err))
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32*1024))
	return TestResult{
		OK: response.StatusCode >= 200 && response.StatusCode < 300, Reachable: true,
		Status: response.StatusCode, DurationMs: time.Since(started).Milliseconds(), URL: target.String(),
	}, nil
}
