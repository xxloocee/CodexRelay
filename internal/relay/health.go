/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 上游代理失败次数的运行时观察
 * @File          : 令牌错误 streak 与短窗口异常统计
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package relay

import (
	"net/http"
	"time"

	"codexrelay/internal/config"
)

const (
	AuthFailureThreshold     = 5
	UpstreamFailureThreshold = 5
	UpstreamFailureWindow    = 3 * time.Minute
)

// HealthSnapshot 是单个本地代理 Profile 的运行时错误摘要，不写入 usage.json 或 config.json。
type HealthSnapshot struct {
	ProfileID         string    `json:"profileId"`
	Category          string    `json:"category"`
	AuthFailures      int       `json:"authFailures"`
	UpstreamFailures  int       `json:"upstreamFailures"`
	AuthTriggered     bool      `json:"authTriggered"`
	UpstreamTriggered bool      `json:"upstreamTriggered"`
	TriggerGeneration uint64    `json:"triggerGeneration"`
	LastStatus        int       `json:"lastStatus"`
	LastFailureAt     time.Time `json:"lastFailureAt"`
}

type upstreamFailureEvent struct {
	at   time.Time
	kind string
}

type profileHealthState struct {
	snapshot           HealthSnapshot
	authFailures       int
	authFailureStatus  int
	upstreamErrors     []upstreamFailureEvent
	authGeneration     uint64
	upstreamGeneration uint64
}

// SetHealthChangedHandler 注册错误阈值变化通知；回调不在健康状态锁内执行。
func (r *Runtime) SetHealthChangedHandler(handler func()) {
	r.healthMu.Lock()
	r.healthChanged = handler
	r.healthMu.Unlock()
}

// SetResultObservedHandler 注册每次上游结果回调；回调不在健康状态锁内执行。
// 调用方可用成功结果清理业务层的故障轮次，但不能改变本次请求已经返回的结果。
func (r *Runtime) SetResultObservedHandler(handler func(profileID, category string, status int, transportError bool)) {
	r.healthMu.Lock()
	r.resultObserved = handler
	r.healthMu.Unlock()
}

// ObserveUpstreamResult 记录一次已收到的上游响应或连接错误。
// 401 与 403 各自要求连续达到阈值；状态码切换或其他结果都会重新开始认证错误计数。5xx 和传输错误按配置窗口统计。
func (r *Runtime) ObserveUpstreamResult(profileID, category string, status int, transportError bool) HealthSnapshot {
	if profileID == "" {
		return HealthSnapshot{}
	}
	now := time.Now()
	r.healthMu.Lock()
	if r.health == nil {
		r.health = make(map[string]*profileHealthState)
	}
	entry := r.health[profileID]
	if entry == nil {
		entry = &profileHealthState{snapshot: HealthSnapshot{ProfileID: profileID, Category: category}}
		r.health[profileID] = entry
	}
	settings := r.tokenSwitchSettings()
	previousAuth := entry.snapshot.AuthTriggered
	previousUpstream := entry.snapshot.UpstreamTriggered
	entry.snapshot.Category = category
	entry.snapshot.LastStatus = status
	entry.snapshot.LastFailureAt = now
	authEnabled := (status == http.StatusUnauthorized && settings.Trigger401) ||
		(status == http.StatusForbidden && settings.Trigger403)
	if authEnabled {
		if entry.authFailureStatus == status {
			entry.authFailures++
		} else {
			entry.authFailureStatus = status
			entry.authFailures = 1
		}
		entry.snapshot.AuthFailures = entry.authFailures
	} else {
		entry.authFailures = 0
		entry.authFailureStatus = 0
		entry.snapshot.AuthFailures = 0
	}
	if !transportError && status < 400 {
		// 成功请求表示当前令牌恢复可用；清空此前累计的 5xx/连接错误，避免恢复后继续沿用旧轮次。
		entry.upstreamErrors = nil
	} else if transportError && settings.TriggerNetwork {
		entry.upstreamErrors = append(entry.upstreamErrors, upstreamFailureEvent{at: now, kind: "network"})
	} else if !transportError && status >= 500 && settings.Trigger5xx {
		entry.upstreamErrors = append(entry.upstreamErrors, upstreamFailureEvent{at: now, kind: "5xx"})
	}
	cutoff := now.Add(-time.Duration(settings.UpstreamFailureWindowMinutes) * time.Minute)
	kept := entry.upstreamErrors[:0]
	for _, event := range entry.upstreamErrors {
		if !event.at.Before(cutoff) {
			kept = append(kept, event)
		}
	}
	entry.upstreamErrors = kept
	entry.snapshot.UpstreamFailures = upstreamFailureCount(kept, settings)
	entry.snapshot.AuthTriggered = authFailureTriggered(entry, settings)
	entry.snapshot.UpstreamTriggered = entry.snapshot.UpstreamFailures >= settings.UpstreamFailureThreshold
	if entry.snapshot.AuthTriggered && !previousAuth {
		entry.authGeneration++
	}
	if entry.snapshot.UpstreamTriggered && !previousUpstream {
		entry.upstreamGeneration++
	}
	if entry.snapshot.AuthTriggered {
		entry.snapshot.TriggerGeneration = entry.authGeneration
	} else if entry.snapshot.UpstreamTriggered {
		entry.snapshot.TriggerGeneration = entry.upstreamGeneration
	} else {
		entry.snapshot.TriggerGeneration = 0
	}
	changed := previousAuth != entry.snapshot.AuthTriggered || previousUpstream != entry.snapshot.UpstreamTriggered
	snapshot := entry.snapshot
	handler := r.healthChanged
	resultHandler := r.resultObserved
	r.healthMu.Unlock()
	if resultHandler != nil {
		resultHandler(profileID, category, status, transportError)
	}
	if changed && handler != nil {
		handler()
	}
	return snapshot
}

// HealthSnapshots 返回当前运行时错误摘要，并清理已超过统计窗口的 5xx/连接错误。
func (r *Runtime) HealthSnapshots() []HealthSnapshot {
	now := time.Now()
	r.healthMu.Lock()
	defer r.healthMu.Unlock()
	result := make([]HealthSnapshot, 0, len(r.health))
	settings := r.tokenSwitchSettings()
	cutoff := now.Add(-time.Duration(settings.UpstreamFailureWindowMinutes) * time.Minute)
	for profileID, entry := range r.health {
		kept := entry.upstreamErrors[:0]
		for _, event := range entry.upstreamErrors {
			if !event.at.Before(cutoff) {
				kept = append(kept, event)
			}
		}
		entry.upstreamErrors = kept
		entry.snapshot.UpstreamFailures = upstreamFailureCount(kept, settings)
		entry.snapshot.AuthTriggered = authFailureTriggered(entry, settings)
		entry.snapshot.UpstreamTriggered = entry.snapshot.UpstreamFailures >= settings.UpstreamFailureThreshold
		if entry.snapshot.AuthTriggered {
			entry.snapshot.TriggerGeneration = entry.authGeneration
		} else if entry.snapshot.UpstreamTriggered {
			entry.snapshot.TriggerGeneration = entry.upstreamGeneration
		} else {
			entry.snapshot.TriggerGeneration = 0
		}
		if entry.authFailures == 0 && len(kept) == 0 {
			delete(r.health, profileID)
			continue
		}
		result = append(result, entry.snapshot)
	}
	return result
}

func (r *Runtime) tokenSwitchSettings() config.TokenSwitchSettings {
	settings := config.DefaultTokenSwitchSettings()
	if state := r.state.Load(); state != nil {
		settings = state.Config.TokenSwitch
		if settings.Mode == "" {
			settings = config.DefaultTokenSwitchSettings()
		}
		if settings.AuthFailureThreshold < 1 {
			settings.AuthFailureThreshold = config.DefaultAuthFailureThreshold
		}
		if settings.UpstreamFailureThreshold < 1 {
			settings.UpstreamFailureThreshold = config.DefaultUpstreamFailureThreshold
		}
		if settings.UpstreamFailureWindowMinutes < 1 {
			settings.UpstreamFailureWindowMinutes = config.DefaultUpstreamFailureWindowMinutes
		}
	}
	return settings
}

func authFailureTriggered(entry *profileHealthState, settings config.TokenSwitchSettings) bool {
	if entry.authFailures < settings.AuthFailureThreshold {
		return false
	}
	return (settings.Trigger401 && entry.snapshot.LastStatus == 401) ||
		(settings.Trigger403 && entry.snapshot.LastStatus == 403)
}

func upstreamFailureCount(events []upstreamFailureEvent, settings config.TokenSwitchSettings) int {
	count := 0
	for _, event := range events {
		switch event.kind {
		case "5xx":
			if settings.Trigger5xx {
				count++
			}
		case "network":
			if settings.TriggerNetwork {
				count++
			}
		}
	}
	return count
}

// ClearHealthForTokenSwitchChanges 清理刚刚变更开关对应的运行时失败记录。
// 调用方必须先持久化新设置再调用；此方法不触发回调，由调用方在清理自动轮次后统一重新评估，避免中间状态产生提示。
func (r *Runtime) ClearHealthForTokenSwitchChanges(previous, current config.TokenSwitchSettings) {
	trigger401Changed := previous.Trigger401 != current.Trigger401
	trigger403Changed := previous.Trigger403 != current.Trigger403
	trigger5xxChanged := previous.Trigger5xx != current.Trigger5xx
	triggerNetworkChanged := previous.TriggerNetwork != current.TriggerNetwork
	if !trigger401Changed && !trigger403Changed && !trigger5xxChanged && !triggerNetworkChanged {
		return
	}

	r.healthMu.Lock()
	defer r.healthMu.Unlock()
	for profileID, entry := range r.health {
		if entry == nil {
			delete(r.health, profileID)
			continue
		}
		if (trigger401Changed && entry.authFailureStatus == http.StatusUnauthorized) ||
			(trigger403Changed && entry.authFailureStatus == http.StatusForbidden) {
			entry.authFailures = 0
			entry.authFailureStatus = 0
		}
		if trigger5xxChanged || triggerNetworkChanged {
			kept := entry.upstreamErrors[:0]
			for _, event := range entry.upstreamErrors {
				if (trigger5xxChanged && event.kind == "5xx") || (triggerNetworkChanged && event.kind == "network") {
					continue
				}
				kept = append(kept, event)
			}
			entry.upstreamErrors = kept
		}

		entry.snapshot.AuthFailures = entry.authFailures
		entry.snapshot.UpstreamFailures = upstreamFailureCount(entry.upstreamErrors, current)
		entry.snapshot.AuthTriggered = authFailureTriggered(entry, current)
		entry.snapshot.UpstreamTriggered = entry.snapshot.UpstreamFailures >= current.UpstreamFailureThreshold
		switch {
		case entry.snapshot.AuthTriggered:
			entry.snapshot.TriggerGeneration = entry.authGeneration
		case entry.snapshot.UpstreamTriggered:
			entry.snapshot.TriggerGeneration = entry.upstreamGeneration
		default:
			entry.snapshot.TriggerGeneration = 0
		}
		if entry.authFailures == 0 && len(entry.upstreamErrors) == 0 {
			delete(r.health, profileID)
		}
	}
}

// ResetProfileHealth 清除指定 Profile 的失败统计；切换成功或手动切换后调用。
func (r *Runtime) ResetProfileHealth(profileID string) {
	if profileID == "" {
		return
	}
	r.healthMu.Lock()
	_, existed := r.health[profileID]
	delete(r.health, profileID)
	handler := r.healthChanged
	r.healthMu.Unlock()
	if existed && handler != nil {
		handler()
	}
}
