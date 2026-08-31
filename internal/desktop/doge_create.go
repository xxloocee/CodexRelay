package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"codexrelay/internal/config"
)

// CreateDogeToken 使用当前绑定的 User 权限令牌创建一个新的不限额、永不过期 API 密钥。
// New API 的创建接口不返回令牌 ID，创建后通过目录差异定位新令牌，再用现有密钥接口读取完整 key。
func (s *DesktopService) CreateDogeToken(input DogeTokenCreateInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Group = strings.TrimSpace(input.Group)
	if input.Name == "" {
		return errors.New("API 密钥名称不能为空")
	}
	if len(input.Name) > 50 {
		return errors.New("API 密钥名称不能超过 50 字节（中文通常占 3 字节）")
	}
	if input.Group == "" {
		return errors.New("请选择 API 密钥分组")
	}

	s.dogeMu.Lock()
	defer s.dogeMu.Unlock()
	state := s.runtime.State()
	if state == nil || strings.TrimSpace(state.Config.Doge.AccessToken) == "" {
		return errors.New("请先绑定二狗子访问令牌")
	}
	if !dogeContainsString(state.Config.Doge.Groups, input.Group) {
		return errors.New("所选分组当前不可用，请先刷新目录")
	}
	baseURL := strings.TrimSpace(state.Config.Doge.BaseURL)
	if baseURL == "" {
		baseURL = defaultDogeBaseURL
	}
	client, err := s.newDogeHTTPClient()
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()

	before, err := s.fetchDogeTokens(context.Background(), client, baseURL, state.Config.Doge.AccessToken)
	if err != nil {
		return fmt.Errorf("读取现有令牌目录失败: %w", err)
	}
	knownIDs := make(map[int64]struct{}, len(before))
	for _, token := range before {
		knownIDs[token.ID] = struct{}{}
	}
	payload := map[string]any{
		"name":            input.Name,
		"group":           input.Group,
		"unlimited_quota": true,
		"expired_time":    -1,
		"remain_quota":    0,
	}
	if _, err := s.dogeRequestJSONWithClient(context.Background(), client, baseURL, state.Config.Doge.AccessToken, http.MethodPost, "/api/token/", payload, ""); err != nil {
		if dogeCreateRequestWasRejected(err) {
			return err
		}
		return fmt.Errorf("API 密钥创建结果未知，请勿重复提交；请先手动同步确认: %w", err)
	}
	after, err := s.fetchDogeTokens(context.Background(), client, baseURL, state.Config.Doge.AccessToken)
	if err != nil {
		return fmt.Errorf("API 密钥已创建，请勿重复提交；读取新令牌目录失败，请手动同步: %w", err)
	}
	created, candidateCount := findCreatedDogeToken(after, knownIDs, input.Name)
	if candidateCount == 0 {
		return errors.New("API 密钥已创建，请勿重复提交；暂时无法定位新令牌，请手动同步")
	}
	if candidateCount > 1 {
		return errors.New("API 密钥已创建，请勿重复提交；发现多个同名新令牌，无法唯一定位，请手动同步")
	}
	key, err := s.fetchDogeTokenKey(context.Background(), client, baseURL, state.Config.Doge.AccessToken, created.ID)
	if err != nil {
		return fmt.Errorf("API 密钥已创建，请勿重复提交；读取完整密钥失败，请手动同步: %w", err)
	}
	created.Key = key
	created.GroupDisplayName = strings.TrimSpace(state.Config.Doge.GroupDisplayNames[created.Group])
	if created.GroupDisplayName == "" {
		created.GroupDisplayName = created.Group
	}
	for _, existing := range state.Config.Doge.Tokens {
		if existing.Group == created.Group && existing.GroupRatio > 0 {
			created.GroupRatio = existing.GroupRatio
			break
		}
	}
	created.Note = dogeTokenNote(created)
	if err := s.updateConfig(func(cfg *config.AppConfig) error {
		for index := range cfg.Doge.Tokens {
			if cfg.Doge.Tokens[index].ID != created.ID {
				continue
			}
			created.Category = cfg.Doge.Tokens[index].Category
			if cfg.Doge.Tokens[index].Note != "" {
				created.Note = cfg.Doge.Tokens[index].Note
			}
			cfg.Doge.Tokens[index] = created
			return nil
		}
		tokens := append([]config.DogeToken(nil), cfg.Doge.Tokens...)
		tokens = append(tokens, created)
		cfg.Doge.TokenOrder = mergeDogeTokenOrder(cfg.Doge.TokenOrder, tokens)
		cfg.Doge.Tokens = orderDogeTokens(cfg.Doge.TokenOrder, tokens)
		return nil
	}); err != nil {
		return fmt.Errorf("API 密钥已创建，请勿重复提交；保存本地目录失败，请手动同步: %w", err)
	}
	return nil
}

func dogeContainsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

// 只有结构化应用拒绝和非超时 4xx 能证明远端没有完成创建；其余错误按结果未知处理。
func dogeCreateRequestWasRejected(err error) bool {
	var httpError *dogeHTTPError
	if !errors.As(err, &httpError) {
		return false
	}
	if httpError.StatusCode >= http.StatusOK && httpError.StatusCode < http.StatusMultipleChoices {
		return true
	}
	return httpError.StatusCode >= http.StatusBadRequest &&
		httpError.StatusCode < http.StatusInternalServerError &&
		httpError.StatusCode != http.StatusRequestTimeout
}

func findCreatedDogeToken(tokens []config.DogeToken, knownIDs map[int64]struct{}, name string) (config.DogeToken, int) {
	var found config.DogeToken
	candidateCount := 0
	for _, token := range tokens {
		if token.ID <= 0 {
			continue
		}
		if _, known := knownIDs[token.ID]; known || (name != "" && token.Name != name) {
			continue
		}
		candidateCount++
		if candidateCount == 1 {
			found = token
		}
	}
	return found, candidateCount
}
