/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 上游失败阈值观察回归测试
 * @File          : 令牌认证失败与上游异常窗口测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package relay

import (
	"testing"
	"time"

	"codexrelay/internal/config"
)

func TestObserveUpstreamResultAuthThresholdAndReset(t *testing.T) {
	runtime := &Runtime{health: make(map[string]*profileHealthState)}
	for index := 0; index < AuthFailureThreshold-1; index++ {
		snapshot := runtime.ObserveUpstreamResult("profile", "codex", 403, false)
		if snapshot.AuthTriggered {
			t.Fatalf("auth prompt triggered after %d failures", index+1)
		}
	}
	snapshot := runtime.ObserveUpstreamResult("profile", "codex", 403, false)
	if !snapshot.AuthTriggered || snapshot.AuthFailures != AuthFailureThreshold {
		t.Fatalf("auth threshold snapshot = %+v", snapshot)
	}
	generation := snapshot.TriggerGeneration
	snapshot = runtime.ObserveUpstreamResult("profile", "codex", 200, false)
	if snapshot.AuthTriggered || snapshot.AuthFailures != 0 {
		t.Fatalf("successful request did not reset auth streak = %+v", snapshot)
	}
	for index := 0; index < AuthFailureThreshold; index++ {
		snapshot = runtime.ObserveUpstreamResult("profile", "codex", 401, false)
	}
	if !snapshot.AuthTriggered || snapshot.TriggerGeneration <= generation {
		t.Fatalf("new auth failure should start a new generation = %+v", snapshot)
	}
	mixed := &Runtime{health: make(map[string]*profileHealthState)}
	for index := 0; index < AuthFailureThreshold-1; index++ {
		mixed.ObserveUpstreamResult("mixed", "codex", 401, false)
	}
	snapshot = mixed.ObserveUpstreamResult("mixed", "codex", 403, false)
	if snapshot.AuthTriggered || snapshot.AuthFailures != 1 {
		t.Fatalf("403 should start its own consecutive streak after 401 = %+v", snapshot)
	}
	for index := 0; index < AuthFailureThreshold-1; index++ {
		snapshot = mixed.ObserveUpstreamResult("mixed", "codex", 403, false)
	}
	if !snapshot.AuthTriggered || snapshot.AuthFailures != AuthFailureThreshold {
		t.Fatalf("consecutive 403 failures should reach the threshold independently = %+v", snapshot)
	}
}

func TestObserveUpstreamResultDisabledAuthStatusDoesNotContributeToEnabledStatus(t *testing.T) {
	cfg := config.Default(18765)
	cfg.TokenSwitch.Trigger401 = false
	cfg.TokenSwitch.Trigger403 = true
	runtime := &Runtime{health: make(map[string]*profileHealthState)}
	runtime.state.Store(&State{Config: cfg})
	for index := 0; index < AuthFailureThreshold-1; index++ {
		runtime.ObserveUpstreamResult("profile", "codex", 401, false)
	}
	snapshot := runtime.ObserveUpstreamResult("profile", "codex", 403, false)
	if snapshot.AuthTriggered || snapshot.AuthFailures != 1 {
		t.Fatalf("disabled 401 failures must not contribute to the 403 threshold = %+v", snapshot)
	}
}

func TestObserveUpstreamResultDisabledConditionsDoNotAccumulate(t *testing.T) {
	cfg := config.Default(18765)
	cfg.TokenSwitch.Trigger401 = false
	cfg.TokenSwitch.Trigger403 = false
	cfg.TokenSwitch.Trigger5xx = false
	cfg.TokenSwitch.TriggerNetwork = false
	runtime := &Runtime{health: make(map[string]*profileHealthState)}
	runtime.state.Store(&State{Config: cfg})

	for index := 0; index < UpstreamFailureThreshold; index++ {
		for _, result := range []struct {
			status         int
			transportError bool
		}{{status: 401}, {status: 403}, {status: 502}, {status: 502, transportError: true}} {
			snapshot := runtime.ObserveUpstreamResult("profile", "codex", result.status, result.transportError)
			if snapshot.AuthFailures != 0 || snapshot.UpstreamFailures != 0 || snapshot.AuthTriggered || snapshot.UpstreamTriggered {
				t.Fatalf("disabled condition accumulated failure state = %+v", snapshot)
			}
		}
	}
	if snapshots := runtime.HealthSnapshots(); len(snapshots) != 0 {
		t.Fatalf("disabled conditions retained health snapshots = %+v", snapshots)
	}
}

func TestClearHealthForTokenSwitchChangesDropsChangedTriggerHistory(t *testing.T) {
	previous := config.DefaultTokenSwitchSettings()
	cfg := config.Default(18765)
	cfg.TokenSwitch = previous
	runtime := &Runtime{health: make(map[string]*profileHealthState)}
	runtime.state.Store(&State{Config: cfg})
	for index := 0; index < AuthFailureThreshold-1; index++ {
		runtime.ObserveUpstreamResult("profile", "codex", 401, false)
		runtime.ObserveUpstreamResult("profile", "codex", 502, false)
	}

	current := previous
	current.Trigger401 = false
	current.Trigger5xx = false
	cfg.TokenSwitch = current
	runtime.state.Store(&State{Config: cfg})
	runtime.ClearHealthForTokenSwitchChanges(previous, current)
	if snapshots := runtime.HealthSnapshots(); len(snapshots) != 0 {
		t.Fatalf("changed triggers retained old history = %+v", snapshots)
	}
}

func TestObserveUpstreamResultWindowThreshold(t *testing.T) {
	runtime := &Runtime{health: make(map[string]*profileHealthState)}
	for index := 0; index < UpstreamFailureThreshold-1; index++ {
		snapshot := runtime.ObserveUpstreamResult("profile", "codex", 502, false)
		if snapshot.UpstreamTriggered {
			t.Fatalf("upstream prompt triggered after %d failures", index+1)
		}
	}
	snapshot := runtime.ObserveUpstreamResult("profile", "codex", 502, false)
	if !snapshot.UpstreamTriggered || snapshot.UpstreamFailures != UpstreamFailureThreshold {
		t.Fatalf("upstream threshold snapshot = %+v", snapshot)
	}
	runtime.healthMu.Lock()
	entry := runtime.health["profile"]
	entry.upstreamErrors[0].at = time.Now().Add(-UpstreamFailureWindow - time.Second)
	runtime.healthMu.Unlock()
	snapshots := runtime.HealthSnapshots()
	if len(snapshots) != 1 || snapshots[0].UpstreamTriggered || snapshots[0].UpstreamFailures != UpstreamFailureThreshold-1 {
		t.Fatalf("expired upstream failure was not pruned = %+v", snapshots)
	}
}
