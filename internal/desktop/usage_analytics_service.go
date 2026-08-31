package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDogeQuotaPerUnit     = 500000
	dogeUsageStatusFetchTimeout = 3 * time.Second
)

type DogeUsageAnalyticsQuery struct {
	StartTimestamp int64  `json:"startTimestamp"`
	EndTimestamp   int64  `json:"endTimestamp"`
	Page           int    `json:"page"`
	PageSize       int    `json:"pageSize"`
	TokenName      string `json:"tokenName"`
	ModelName      string `json:"modelName"`
	Group          string `json:"group"`
}

type DogeUsageLog struct {
	ID               int    `json:"id"`
	CreatedAt        int64  `json:"created_at"`
	Type             int    `json:"type"`
	Content          string `json:"content"`
	TokenName        string `json:"token_name"`
	ModelName        string `json:"model_name"`
	Quota            int64  `json:"quota"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	UseTime          int64  `json:"use_time"`
	IsStream         bool   `json:"is_stream"`
	Group            string `json:"group"`
	Other            string `json:"other"`
}

type DogeUsageLogPage struct {
	Page         int            `json:"page"`
	PageSize     int            `json:"page_size"`
	Total        int            `json:"total"`
	Items        []DogeUsageLog `json:"items"`
	QuotaPerUnit float64        `json:"quota_per_unit"`
}

type DogeBillingTokenMetrics struct {
	InputTokens                   int64   `json:"input_tokens"`
	PromptTokens                  int64   `json:"prompt_tokens"`
	CompletionTokens              int64   `json:"completion_tokens"`
	CacheReadTokens               int64   `json:"cache_read_tokens"`
	CacheWriteTokens              int64   `json:"cache_write_tokens"`
	CacheTokens                   int64   `json:"cache_tokens"`
	TotalTokensWithCache          int64   `json:"total_tokens_with_cache"`
	InputShare                    float64 `json:"input_share"`
	CompletionShare               float64 `json:"completion_share"`
	AvgInputTokensPerRequest      float64 `json:"avg_input_tokens_per_request"`
	AvgCompletionTokensPerRequest float64 `json:"avg_completion_tokens_per_request"`
	AvgCacheTokensPerRequest      float64 `json:"avg_cache_tokens_per_request"`
}

type DogeBillingOverviewItem struct {
	Key                       string  `json:"key"`
	Label                     string  `json:"label"`
	Quota                     int64   `json:"quota"`
	OriginalQuota             float64 `json:"original_quota"`
	RequestCount              int64   `json:"request_count"`
	TokenCount                int64   `json:"token_count"`
	EffectiveQuotaPer1MTokens float64 `json:"effective_quota_per_1k_tokens"`
}

type DogeBillingSummary struct {
	TotalQuota                     int64                     `json:"total_quota"`
	OriginalTotalQuota             float64                   `json:"original_total_quota"`
	WalletQuota                    int64                     `json:"wallet_quota"`
	WalletMultiplierOverview       []DogeBillingOverviewItem `json:"wallet_multiplier_overview"`
	SubscriptionQuota              int64                     `json:"subscription_quota"`
	SubscriptionMultiplierOverview []DogeBillingOverviewItem `json:"subscription_multiplier_overview"`
	MultiplierOverview             []DogeBillingOverviewItem `json:"multiplier_overview"`
	TokenCount                     int64                     `json:"token_count"`
	RequestCount                   int64                     `json:"request_count"`
	EffectiveQuotaPer1MTokens      float64                   `json:"effective_quota_per_1k_tokens"`
	TokenMetrics                   DogeBillingTokenMetrics   `json:"token_metrics"`
}

type DogeBillingRow struct {
	Key                       string                  `json:"key"`
	Name                      string                  `json:"name"`
	TotalQuota                int64                   `json:"total_quota"`
	WalletQuota               int64                   `json:"wallet_quota"`
	SubscriptionQuota         int64                   `json:"subscription_quota"`
	TokenCount                int64                   `json:"token_count"`
	RequestCount              int64                   `json:"request_count"`
	EffectiveQuotaPer1MTokens float64                 `json:"effective_quota_per_1k_tokens"`
	LastUsedAt                int64                   `json:"last_used_at"`
	TokenMetrics              DogeBillingTokenMetrics `json:"token_metrics"`
}

type DogeBillingAnalysis struct {
	Summary      DogeBillingSummary `json:"summary"`
	Tokens       []DogeBillingRow   `json:"tokens"`
	Models       []DogeBillingRow   `json:"models"`
	Groups       []DogeBillingRow   `json:"groups"`
	QuotaPerUnit float64            `json:"quota_per_unit"`
}

func (s *DesktopService) GetDogeUsageLogs(input DogeUsageAnalyticsQuery) (DogeUsageLogPage, error) {
	input, err := normalizeDogeUsageAnalyticsQuery(input)
	if err != nil {
		return DogeUsageLogPage{}, err
	}
	baseURL, accessToken, client, err := s.dogeUsageAnalyticsClient()
	if err != nil {
		return DogeUsageLogPage{}, err
	}
	defer client.CloseIdleConnections()

	query := buildDogeUsageAnalyticsQuery(input)
	query.Set("type", "2")
	query.Set("p", strconv.Itoa(input.Page))
	query.Set("page_size", strconv.Itoa(input.PageSize))
	quotaPerUnit := s.fetchDogeQuotaPerUnitAsync(client, baseURL)
	data, err := s.dogeRequestWithClient(context.Background(), client, baseURL, accessToken, http.MethodGet, "/api/log/self?"+query.Encode())
	if err != nil {
		return DogeUsageLogPage{}, err
	}

	var result DogeUsageLogPage
	if err := json.Unmarshal(data, &result); err != nil {
		return DogeUsageLogPage{}, errors.New("二狗子使用日志格式无效")
	}
	if result.Items == nil {
		result.Items = []DogeUsageLog{}
	}
	result.QuotaPerUnit = <-quotaPerUnit
	return result, nil
}

func (s *DesktopService) GetDogeBillingAnalysis(input DogeUsageAnalyticsQuery) (DogeBillingAnalysis, error) {
	input, err := normalizeDogeUsageAnalyticsQuery(input)
	if err != nil {
		return DogeBillingAnalysis{}, err
	}
	baseURL, accessToken, client, err := s.dogeUsageAnalyticsClient()
	if err != nil {
		return DogeBillingAnalysis{}, err
	}
	defer client.CloseIdleConnections()

	query := buildDogeUsageAnalyticsQuery(input)
	quotaPerUnit := s.fetchDogeQuotaPerUnitAsync(client, baseURL)
	path := "/api/billing/analysis/self"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	data, err := s.dogeRequestWithClient(context.Background(), client, baseURL, accessToken, http.MethodGet, path)
	if err != nil {
		return DogeBillingAnalysis{}, err
	}

	var result DogeBillingAnalysis
	if err := json.Unmarshal(data, &result); err != nil {
		return DogeBillingAnalysis{}, errors.New("二狗子计费分析格式无效")
	}
	result.Tokens = nonNilDogeBillingRows(result.Tokens)
	result.Models = nonNilDogeBillingRows(result.Models)
	result.Groups = nonNilDogeBillingRows(result.Groups)
	result.Summary.WalletMultiplierOverview = nonNilDogeBillingOverview(result.Summary.WalletMultiplierOverview)
	result.Summary.SubscriptionMultiplierOverview = nonNilDogeBillingOverview(result.Summary.SubscriptionMultiplierOverview)
	result.Summary.MultiplierOverview = nonNilDogeBillingOverview(result.Summary.MultiplierOverview)
	result.QuotaPerUnit = <-quotaPerUnit
	return result, nil
}

func normalizeDogeUsageAnalyticsQuery(input DogeUsageAnalyticsQuery) (DogeUsageAnalyticsQuery, error) {
	if input.StartTimestamp < 0 || input.EndTimestamp < 0 || (input.StartTimestamp > 0 && input.EndTimestamp > 0 && input.StartTimestamp > input.EndTimestamp) {
		return input, errors.New("用量统计时间范围无效")
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PageSize < 1 || input.PageSize > 100 {
		input.PageSize = 20
	}
	input.TokenName = strings.TrimSpace(input.TokenName)
	input.ModelName = strings.TrimSpace(input.ModelName)
	input.Group = strings.TrimSpace(input.Group)
	return input, nil
}

func buildDogeUsageAnalyticsQuery(input DogeUsageAnalyticsQuery) url.Values {
	query := url.Values{}
	if input.StartTimestamp > 0 {
		query.Set("start_timestamp", strconv.FormatInt(input.StartTimestamp, 10))
	}
	if input.EndTimestamp > 0 {
		query.Set("end_timestamp", strconv.FormatInt(input.EndTimestamp, 10))
	}
	if input.TokenName != "" {
		query.Set("token_name", input.TokenName)
	}
	if input.ModelName != "" {
		query.Set("model_name", input.ModelName)
	}
	if input.Group != "" {
		query.Set("group", input.Group)
	}
	return query
}

func (s *DesktopService) dogeUsageAnalyticsClient() (string, string, *http.Client, error) {
	state := s.runtime.State()
	if state == nil || strings.TrimSpace(state.Config.Doge.AccessToken) == "" {
		return "", "", nil, errors.New("请先绑定二狗子访问令牌")
	}
	client, err := s.newDogeHTTPClient()
	if err != nil {
		return "", "", nil, err
	}
	return state.Config.Doge.BaseURL, state.Config.Doge.AccessToken, client, nil
}

func (s *DesktopService) fetchDogeQuotaPerUnitAsync(client *http.Client, baseURL string) <-chan float64 {
	result := make(chan float64, 1)
	go func() {
		quotaPerUnit := float64(defaultDogeQuotaPerUnit)
		ctx, cancel := context.WithTimeout(context.Background(), dogeUsageStatusFetchTimeout)
		defer cancel()
		if data, err := s.dogeRequestWithClient(ctx, client, baseURL, "", http.MethodGet, "/api/status"); err == nil {
			var status dogeStatusResponse
			if json.Unmarshal(data, &status) == nil && status.QuotaPerUnit > 0 {
				quotaPerUnit = status.QuotaPerUnit
			}
		}
		result <- quotaPerUnit
	}()
	return result
}

func nonNilDogeBillingRows(rows []DogeBillingRow) []DogeBillingRow {
	if rows == nil {
		return []DogeBillingRow{}
	}
	return rows
}

func nonNilDogeBillingOverview(rows []DogeBillingOverviewItem) []DogeBillingOverviewItem {
	if rows == nil {
		return []DogeBillingOverviewItem{}
	}
	return rows
}
