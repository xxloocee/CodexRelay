package desktop

import (
	"strconv"
	"strings"

	"codexrelay/internal/config"
	"codexrelay/internal/tasknotify"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (s *DesktopService) enqueueTaskNotificationEvent(eventType, identity string, details ...tasknotify.EventDetails) {
	if s.taskNotifier == nil {
		return
	}
	if err := s.taskNotifier.Enqueue(eventType, identity, details...); err != nil {
		application.Get().Logger.Warn("创建消息通知待投递记录失败", "error", err)
	}
}

// handleHealthChanged 在健康阈值首次触发时记录独立故障通知，再按设置执行自动切换；失败轮次耗尽后保留停止状态提醒。
// 回调由 relay 在请求完成后调用，因此只改变后续请求使用的活动 Profile，不重放已经失败的请求。
func (s *DesktopService) handleHealthChanged() {
	s.enqueueTokenRequestFailureNotifications()
	s.failoverMu.Lock()
	prompts := s.buildTokenSwitchPrompts()
	previousIDs := make([]string, 0, len(prompts))
	state := s.runtime.State()
	if state != nil && state.Config.TokenSwitch.Mode == config.TokenSwitchModeAuto {
		for _, category := range config.Categories {
			prompt := prompts[category]
			if switched, previousID := s.tryAutomaticTokenSwitch(prompt); switched {
				previousIDs = append(previousIDs, previousID)
			}
		}
	}
	s.failoverMu.Unlock()
	for _, previousID := range previousIDs {
		s.runtime.ResetProfileHealth(previousID)
	}
	s.notifyStateChanged()
}

// enqueueTokenRequestFailureNotifications 为每个达到健康阈值的活动 Profile 创建一条独立通知。
// 该事件不依赖自动切换模式；身份包含触发代数，保证同一故障轮次不会因状态刷新重复入队。
// 只保存类别、故障类别、次数、状态码和时间，不保存令牌名称、密钥或错误正文。
func (s *DesktopService) enqueueTokenRequestFailureNotifications() {
	state := s.runtime.State()
	if state == nil {
		return
	}
	for _, health := range s.runtime.HealthSnapshots() {
		failureKind := ""
		switch {
		case health.AuthTriggered:
			failureKind = "auth"
		case health.UpstreamTriggered:
			failureKind = "upstream"
		default:
			continue
		}
		profileIndex := config.FindProfileIndex(state.Config.Profiles, health.ProfileID)
		if profileIndex < 0 || state.Config.ActiveProfiles[health.Category] != health.ProfileID {
			continue
		}
		identity := strings.Join([]string{health.ProfileID, failureKind, strconv.Itoa(health.LastStatus), strconv.FormatUint(health.TriggerGeneration, 10)}, "\x00")
		s.enqueueTaskNotificationEvent(tasknotify.EventTokenRequestFailed, identity, tasknotify.EventDetails{
			OccurredAt: health.LastFailureAt, Category: health.Category, FailureKind: failureKind,
			FailureCount: func() int {
				if failureKind == "auth" {
					return health.AuthFailures
				}
				return health.UpstreamFailures
			}(), FailureStatus: health.LastStatus, FailureWindowMinutes: state.Config.TokenSwitch.UpstreamFailureWindowMinutes,
		})
	}
}
