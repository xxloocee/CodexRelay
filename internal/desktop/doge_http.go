package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codexrelay/internal/network"
	"codexrelay/internal/relay"
)

func (s *DesktopService) dogeRequest(ctx context.Context, baseURL, accessToken, method, path string) ([]byte, error) {
	return s.dogeRequestBody(ctx, baseURL, accessToken, method, path, nil, "")
}

func (s *DesktopService) dogeRequestJSON(ctx context.Context, baseURL, accessToken, method, path string, payload any, sensitive string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("创建二狗子请求体失败: %w", err)
	}
	return s.dogeRequestBody(ctx, baseURL, accessToken, method, path, bytes.NewReader(body), sensitive)
}

func (s *DesktopService) dogeRequestBody(ctx context.Context, baseURL, accessToken, method, path string, body io.Reader, sensitive string) ([]byte, error) {
	client, err := s.newDogeHTTPClient()
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()
	return s.dogeRequestBodyWithClient(ctx, client, baseURL, accessToken, method, path, body, sensitive)
}

func (s *DesktopService) dogeRequestWithClient(ctx context.Context, client *http.Client, baseURL, accessToken, method, path string) ([]byte, error) {
	return s.dogeRequestBodyWithClient(ctx, client, baseURL, accessToken, method, path, nil, "")
}

func (s *DesktopService) dogeRequestJSONWithClient(ctx context.Context, client *http.Client, baseURL, accessToken, method, path string, payload any, sensitive string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("创建二狗子请求体失败: %w", err)
	}
	return s.dogeRequestBodyWithClient(ctx, client, baseURL, accessToken, method, path, bytes.NewReader(body), sensitive)
}

func (s *DesktopService) newDogeHTTPClient() (*http.Client, error) {
	state := s.runtime.State()
	if state == nil {
		return nil, errors.New("程序尚未初始化")
	}
	transport, err := network.BuildTransport(state.Config.Network, network.DetectSystemProxy(), state.Config.ProxyPort)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}, nil
}

func (s *DesktopService) dogeRequestBodyWithClient(ctx context.Context, client *http.Client, baseURL, accessToken, method, path string, body io.Reader, sensitive string) ([]byte, error) {
	target, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || target.Host == "" {
		return nil, errors.New("二狗子 API 地址无效")
	}
	requestPath, query, hasQuery := strings.Cut(path, "?")
	target.Path = strings.TrimRight(target.Path, "/") + requestPath
	if hasQuery {
		target.RawQuery = query
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("创建二狗子请求失败: %w", err)
	}
	if strings.TrimSpace(accessToken) != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("二狗子请求失败: %s", relay.SanitizeError(err))
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取二狗子响应失败: %w", err)
	}
	var envelope dogeEnvelope
	if decodeErr := json.Unmarshal(responseBody, &envelope); decodeErr != nil {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, &dogeHTTPError{StatusCode: response.StatusCode}
		}
		return nil, fmt.Errorf("二狗子响应格式无效: %w", decodeErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || (!envelope.Success && envelope.Message != "") {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = "请求被拒绝"
		}
		if accessToken != "" {
			message = strings.ReplaceAll(message, accessToken, "[已隐藏]")
		}
		if sensitive != "" {
			message = strings.ReplaceAll(message, sensitive, "[已隐藏]")
		}
		return nil, &dogeHTTPError{StatusCode: response.StatusCode, Message: message}
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return []byte("null"), nil
	}
	return envelope.Data, nil
}
