package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"codexrelay/internal/config"
)

func (s *DesktopService) fetchDogeData(ctx context.Context, baseURL, accessToken string, previous []config.DogeToken, mode dogeSyncMode, client *http.Client) (config.DogeConnection, dogeAnnouncementSnapshot, error) {
	var (
		user          map[string]any
		account       config.DogeAccount
		groups        []string
		groupInfo     map[string]dogeGroupInfo
		tokens        []config.DogeToken
		subscriptions []config.DogeSubscription
		topup         config.DogeTopupInfo
		announcements dogeAnnouncementSnapshot
	)
	errorsCh := make(chan error, 6)
	var waitGroup sync.WaitGroup
	waitGroup.Add(6)
	go func() {
		defer waitGroup.Done()
		var err error
		user, account, err = s.fetchDogeUser(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子用户信息失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		groups, groupInfo, err = s.fetchDogeGroups(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子分组失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		tokens, err = s.fetchDogeTokens(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子令牌目录失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		subscriptions, err = s.fetchDogeSubscriptions(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子套餐失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		topup, err = s.fetchDogeTopupInfo(ctx, client, baseURL, accessToken)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子兑换配置失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		announcements, err = s.fetchDogeAnnouncements(ctx, client, baseURL)
		if err != nil {
			errorsCh <- fmt.Errorf("同步二狗子公告失败: %w", err)
		}
	}()
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		return config.DogeConnection{}, dogeAnnouncementSnapshot{}, err
	}

	// 基础数据全部完成后，才进入令牌密钥阶段；元数据同步若没有新增密钥，会直接跳过该阶段。
	s.setDogeSyncing(true, false)
	if count := countDogeTokenKeys(tokens, previous, mode); count > 0 {
		s.setDogeSyncPhase(dogeSyncPhaseKeys)
	}
	if err := s.syncDogeTokenKeys(ctx, client, baseURL, accessToken, tokens, previous, mode); err != nil {
		return config.DogeConnection{}, dogeAnnouncementSnapshot{}, err
	}
	previousByID := make(map[int64]config.DogeToken, len(previous))
	for _, token := range previous {
		if token.ID > 0 {
			token.Key = normalizeDogeAPIKey(token.Key)
			previousByID[token.ID] = token
		}
	}
	for i := range tokens {
		if previousToken, ok := previousByID[tokens[i].ID]; ok {
			tokens[i].Category = previousToken.Category
			tokens[i].Note = previousToken.Note
			tokens[i].GroupDisplayName = previousToken.GroupDisplayName
			tokens[i].GroupRatio = previousToken.GroupRatio
		}
		if info, ok := groupInfo[tokens[i].Group]; ok {
			tokens[i].GroupDisplayName = info.DisplayName
			tokens[i].GroupRatio = info.Ratio
		} else if len(groupInfo) > 0 {
			// 官方对象响应是当前完整分组目录；找不到的组不能继续沿用旧展示名或倍率。
			tokens[i].GroupDisplayName = ""
			tokens[i].GroupRatio = 0
		}
		if tokens[i].Note == "" {
			tokens[i].Note = dogeTokenNote(tokens[i])
		}
	}
	return config.DogeConnection{BaseURL: baseURL, User: user, Account: account, Subscriptions: subscriptions, Topup: topup, Groups: groups, GroupDisplayNames: dogeGroupDisplayNames(groups, groupInfo), Tokens: tokens, LastSyncAt: time.Now()}, announcements, nil
}

// syncDogeTokenKeys 将令牌列表中的掩码与本地完整密钥分开处理。
// 远端令牌 ID 是唯一稳定身份：完整同步刷新所有令牌，元数据同步只请求本地缺少密钥的令牌。
func (s *DesktopService) syncDogeTokenKeys(ctx context.Context, client *http.Client, baseURL, accessToken string, tokens, previous []config.DogeToken, mode dogeSyncMode) error {
	previousKeys := make(map[int64]string, len(previous))
	for _, token := range previous {
		if token.ID > 0 {
			previousKeys[token.ID] = normalizeDogeAPIKey(token.Key)
		}
	}
	pending := make([]int, 0, len(tokens))
	for index := range tokens {
		key := previousKeys[tokens[index].ID]
		if mode == dogeSyncMetadata && isCompleteDogeAPIKey(key) {
			tokens[index].Key = key
			continue
		}
		pending = append(pending, index)
	}
	if len(pending) == 0 {
		return nil
	}
	// 每批请求必须全部结束后才能启动下一批，避免滑动窗口持续补发请求。
	for start := 0; start < len(pending); start += dogeTokenKeyConcurrency {
		end := minInt(start+dogeTokenKeyConcurrency, len(pending))
		batchCtx, cancel := context.WithCancel(ctx)
		var batchWaitGroup sync.WaitGroup
		var firstErr error
		var firstErrOnce sync.Once
		for _, index := range pending[start:end] {
			batchWaitGroup.Add(1)
			go func(index int) {
				defer batchWaitGroup.Done()
				key, err := s.fetchDogeTokenKey(batchCtx, client, baseURL, accessToken, tokens[index].ID)
				if err != nil {
					firstErrOnce.Do(func() {
						firstErr = fmt.Errorf("同步二狗子令牌 %d 密钥失败: %w", tokens[index].ID, err)
						cancel()
					})
					return
				}
				tokens[index].Key = normalizeDogeAPIKey(key)
				if !isCompleteDogeAPIKey(tokens[index].Key) {
					firstErrOnce.Do(func() {
						firstErr = fmt.Errorf("二狗子令牌 %d 没有返回完整 API 密钥", tokens[index].ID)
						cancel()
					})
				}
			}(index)
		}
		batchWaitGroup.Wait()
		cancel()
		if firstErr != nil {
			return firstErr
		}
	}
	return nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func countDogeTokenKeys(tokens, previous []config.DogeToken, mode dogeSyncMode) int {
	previousKeys := make(map[int64]string, len(previous))
	for _, token := range previous {
		if token.ID > 0 {
			previousKeys[token.ID] = normalizeDogeAPIKey(token.Key)
		}
	}
	count := 0
	for _, token := range tokens {
		if mode == dogeSyncFull || !isCompleteDogeAPIKey(previousKeys[token.ID]) {
			count++
		}
	}
	return count
}

// fetchDogeAnnouncements 并行获取公告列表和当前公告；结果由账户同步与独立公告同步共同复用。
func (s *DesktopService) fetchDogeAnnouncements(ctx context.Context, client *http.Client, baseURL string) (dogeAnnouncementSnapshot, error) {
	var statusData, noticeData []byte
	errorsCh := make(chan error, 2)
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		var err error
		statusData, err = s.dogeRequestWithClient(ctx, client, baseURL, "", http.MethodGet, "/api/status")
		if err != nil {
			if isDogeNotFound(err) {
				statusData = []byte(`{}`)
				return
			}
			errorsCh <- fmt.Errorf("二狗子公告状态请求失败: %w", err)
		}
	}()
	go func() {
		defer waitGroup.Done()
		var err error
		noticeData, err = s.dogeRequestWithClient(ctx, client, baseURL, "", http.MethodGet, "/api/notice")
		if err != nil {
			if isDogeNotFound(err) {
				noticeData = []byte("null")
				return
			}
			errorsCh <- fmt.Errorf("二狗子当前公告请求失败: %w", err)
		}
	}()
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		return dogeAnnouncementSnapshot{}, err
	}
	var status dogeStatusResponse
	if err := json.Unmarshal(statusData, &status); err != nil {
		return dogeAnnouncementSnapshot{}, fmt.Errorf("二狗子公告状态格式无效: %w", err)
	}
	var currentNotice string
	if len(noticeData) > 0 && string(noticeData) != "null" {
		if err := json.Unmarshal(noticeData, &currentNotice); err != nil {
			return dogeAnnouncementSnapshot{}, fmt.Errorf("二狗子当前公告格式无效: %w", err)
		}
	}
	return dogeAnnouncementSnapshot{Status: status, CurrentNotice: currentNotice}, nil
}

func (s *DesktopService) fetchDogeUser(ctx context.Context, client *http.Client, baseURL, accessToken string) (map[string]any, config.DogeAccount, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, "/api/user/self")
	if err != nil {
		return nil, config.DogeAccount{}, err
	}
	var user map[string]any
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, config.DogeAccount{}, fmt.Errorf("二狗子用户信息格式无效: %w", err)
	}
	var typed dogeUserResponse
	if err := json.Unmarshal(data, &typed); err != nil {
		return nil, config.DogeAccount{}, fmt.Errorf("二狗子用户额度格式无效: %w", err)
	}
	return sanitizeDogeUser(user), config.DogeAccount{ID: typed.ID, Username: typed.Username, DisplayName: typed.DisplayName, Email: typed.Email, Group: typed.Group, Status: typed.Status, Quota: typed.Quota, UsedQuota: typed.UsedQuota, RequestCount: typed.RequestCount}, nil
}

func (s *DesktopService) fetchDogeSubscriptions(ctx context.Context, client *http.Client, baseURL, accessToken string) ([]config.DogeSubscription, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, "/api/subscription/self")
	if err != nil {
		if isDogeNotFound(err) {
			return []config.DogeSubscription{}, nil
		}
		return nil, err
	}
	var payload dogeSubscriptionResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("二狗子套餐信息格式无效: %w", err)
	}
	result := make([]config.DogeSubscription, 0, len(payload.Subscriptions))
	for _, item := range payload.Subscriptions {
		result = append(result, config.DogeSubscription{ID: item.Subscription.ID, PlanID: item.Subscription.PlanID, PlanTitle: item.Plan.Title, Status: item.Subscription.Status, AmountTotal: item.Subscription.AmountTotal, AmountUsed: item.Subscription.AmountUsed, StartTime: item.Subscription.StartTime, EndTime: item.Subscription.EndTime})
	}
	return result, nil
}

func (s *DesktopService) fetchDogeTopupInfo(ctx context.Context, client *http.Client, baseURL, accessToken string) (config.DogeTopupInfo, error) {
	data, err := s.dogeRequestWithClient(ctx, client, baseURL, accessToken, http.MethodGet, "/api/user/topup/info")
	if err != nil {
		if isDogeNotFound(err) {
			return config.DogeTopupInfo{}, nil
		}
		return config.DogeTopupInfo{}, err
	}
	var payload dogeTopupInfoResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return config.DogeTopupInfo{}, fmt.Errorf("二狗子充值配置格式无效: %w", err)
	}
	return config.DogeTopupInfo{EnableRedemption: payload.EnableRedemption, TopupLink: strings.TrimSpace(payload.TopupLink)}, nil
}
