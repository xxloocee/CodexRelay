/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 上游模型目录获取、校验与代理 API 模型缓存转换
 * @File          : 模型目录服务
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/network"
	"codexrelay/internal/relay"
)

const modelFetchBodyLimit = 4 << 20

type modelListResponse struct {
	Data []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

func publicModels(models []config.ModelEntry) []PublicModel {
	if len(models) == 0 {
		return []PublicModel{}
	}
	result := make([]PublicModel, 0, len(models))
	for _, model := range models {
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = model.ID
		}
		result = append(result, PublicModel{ID: model.ID, Name: name, OwnedBy: model.OwnedBy, ContextWindow: model.ContextWindow})
	}
	return result
}

func modelInputsToConfig(models []ModelInput) []config.ModelEntry {
	if models == nil {
		return nil
	}
	if len(models) == 0 {
		return []config.ModelEntry{}
	}
	result := make([]config.ModelEntry, 0, len(models))
	for _, model := range models {
		result = append(result, config.ModelEntry{ID: strings.TrimSpace(model.ID), Name: strings.TrimSpace(model.Name), OwnedBy: strings.TrimSpace(model.OwnedBy), ContextWindow: model.ContextWindow})
	}
	return result
}

func configModelsToInput(models []config.ModelEntry) []ModelInput {
	if models == nil {
		return nil
	}
	if len(models) == 0 {
		return []ModelInput{}
	}
	result := make([]ModelInput, 0, len(models))
	for _, model := range models {
		result = append(result, ModelInput{ID: model.ID, Name: model.Name, OwnedBy: model.OwnedBy, ContextWindow: model.ContextWindow})
	}
	return result
}

func buildModelFetchHeaders(request *http.Request, apiKey string, headers map[string]string) {
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	for name, value := range headers {
		if strings.EqualFold(strings.TrimSpace(name), "Authorization") || strings.EqualFold(strings.TrimSpace(name), "Host") {
			continue
		}
		request.Header.Set(name, value)
	}
}

func validateModelFetchHeaders(headers map[string]string) error {
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "\r\n") {
			return errors.New("额外请求头名称无效")
		}
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Host") {
			return fmt.Errorf("%s 由代理管理，不能作为额外请求头", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("请求头 %s 的值不能包含换行", name)
		}
	}
	return nil
}

// modelEndpointCandidates 按 CC Switch 的通用 OpenAI 兼容路径顺序构造候选地址；不会遍历未知路径。
func modelEndpointCandidates(raw string) ([]string, error) {
	base, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return nil, errors.New("API 地址必须是有效的 http:// 或 https:// 地址")
	}
	base.RawQuery = ""
	base.Fragment = ""
	seen := map[string]struct{}{}
	result := make([]string, 0, 5)
	add := func(path string) {
		copyURL := *base
		copyURL.Path = relay.JoinTargetPath(base.Path, path)
		copyURL.RawPath = ""
		value := copyURL.String()
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	basePath := strings.TrimRight(base.Path, "/")
	if strings.HasSuffix(strings.ToLower(basePath), "/v1") {
		add("/models")
	}
	add("/v1/models")
	add("/models")
	// 对明确版本路径保留一次去版本候选，覆盖 /api/v2 这类兼容网关而不做模糊扫描。
	segments := strings.Split(strings.Trim(basePath, "/"), "/")
	if len(segments) > 0 {
		last := strings.ToLower(segments[len(segments)-1])
		if len(last) > 1 && last[0] == 'v' {
			allDigits := true
			for _, char := range last[1:] {
				if char < '0' || char > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				root := strings.Join(segments[:len(segments)-1], "/")
				copyURL := *base
				copyURL.Path = "/" + strings.Trim(root, "/")
				copyURL.RawPath = ""
				rootURL := strings.TrimRight(copyURL.String(), "/")
				for _, suffix := range []string{"/models", "/v1/models"} {
					value := rootURL + suffix
					if _, ok := seen[value]; !ok {
						seen[value] = struct{}{}
						result = append(result, value)
					}
				}
			}
		}
	}
	return result, nil
}

func parseModelList(data []byte) ([]PublicModel, error) {
	var payload modelListResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]PublicModel, 0, len(payload.Data))
	for _, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, PublicModel{ID: id, Name: id, OwnedBy: strings.TrimSpace(entry.OwnedBy)})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	if len(models) == 0 {
		return nil, errors.New("上游未返回可用模型")
	}
	return models, nil
}

func (s *DesktopService) fetchModels(baseURL, apiKey string, headers map[string]string) ([]PublicModel, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("API 密钥不能为空")
	}
	if err := validateModelFetchHeaders(headers); err != nil {
		return nil, err
	}
	candidates, err := modelEndpointCandidates(baseURL)
	if err != nil {
		return nil, err
	}
	state := s.runtime.State()
	if state == nil {
		return nil, errors.New("程序尚未初始化")
	}
	transport, err := network.BuildTransport(state.Config.Network, network.DetectSystemProxy(), state.Config.ProxyPort)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	var lastErr error
	for _, endpoint := range candidates {
		request, requestErr := http.NewRequest(http.MethodGet, endpoint, nil)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		buildModelFetchHeaders(request, apiKey, headers)
		response, requestErr := client.Do(request)
		if requestErr != nil {
			lastErr = fmt.Errorf("连接模型目录失败: %w", requestErr)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, modelFetchBodyLimit))
		_ = response.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("读取模型目录失败: %w", readErr)
			continue
		}
		if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
			lastErr = fmt.Errorf("模型目录地址返回 HTTP %d", response.StatusCode)
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("模型目录请求失败：HTTP %d", response.StatusCode)
		}
		models, parseErr := parseModelList(body)
		if parseErr != nil {
			return nil, parseErr
		}
		return models, nil
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的模型目录地址")
	}
	return nil, lastErr
}

// FetchProfileModels 获取当前编辑器输入对应的上游模型目录；只读上游，不修改本地配置。
func (s *DesktopService) FetchProfileModels(input ProfileInput) ([]PublicModel, error) {
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if input.APIKey == "" && strings.TrimSpace(input.ID) != "" {
		if state := s.runtime.State(); state != nil {
			if index := config.FindProfileIndex(state.Config.Profiles, strings.TrimSpace(input.ID)); index >= 0 {
				stored := state.Config.Profiles[index]
				// 复用后端密钥时，请求地址和额外请求头也必须来自同一个
				// 已保存 Profile，不能把密钥发送到前端指定的任意地址。
				input.APIKey = strings.TrimSpace(stored.APIKey)
				input.BaseURL = stored.BaseURL
				input.Headers = stored.Headers
			}
		}
	}
	return s.fetchModels(input.BaseURL, input.APIKey, input.Headers)
}
