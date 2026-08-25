/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 当前配置格式与持久化回归测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreIgnoresUnknownConfigFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{
  "version": 999,
  "proxyPort": 8765,
  "localAccessToken": "sk-placeholder",
  "activeProfiles": {},
  "profiles": [],
  "network": {"mode": "system"},
  "preferences": {}
}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).LoadOrCreate(0); err != nil {
		t.Fatalf("未知配置字段不应阻止加载: %v", err)
	}
}

func TestStoreLoadsCurrentPlaintextFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := NewStore(path)
	cfg := Default(18765)
	cfg.Profiles = []Profile{{
		ID: "one", Source: SourceCustom, Category: CategoryCodex, Name: "One", BaseURL: "https://example.test/v1",
		APIKey: "sk-plaintext-placeholder", Note: "主线路",
	}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(saved, []byte(`"version"`)) {
		t.Fatal("配置文件不应写入 version 字段")
	}
	var raw map[string]any
	if err := json.Unmarshal(saved, &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["version"]; exists {
		t.Fatal("配置文件不应包含 version 字段")
	}
	loaded, err := store.LoadOrCreate(0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Profiles[0].APIKey != "sk-plaintext-placeholder" || loaded.Profiles[0].Note != "主线路" {
		t.Fatalf("current profile = %+v", loaded.Profiles[0])
	}
}

func TestDefaultLocalAccessTokenStartsWithSK(t *testing.T) {
	cfg := Default(0)
	if !strings.HasPrefix(cfg.LocalAccessToken, "sk-") {
		t.Fatalf("local access token = %q", cfg.LocalAccessToken)
	}
	if len(cfg.Preferences.VisibleCategories) != len(Categories) || cfg.Preferences.DefaultSource != "" || cfg.Preferences.DefaultCategory != CategoryCodex || cfg.Preferences.RestoreViewMode != RestoreViewCurrent {
		t.Fatalf("preference defaults = %+v", cfg.Preferences)
	}
}

func TestStoreCompletesViewPreferenceDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{
  "proxyPort": 8765,
  "localAccessToken": "sk-placeholder",
  "activeProfiles": {},
  "profiles": [],
  "network": {"mode": "system"},
  "preferences": {}
}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewStore(path).LoadOrCreate(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Preferences.VisibleCategories) != len(Categories) || cfg.Preferences.RestoreViewMode != RestoreViewCurrent {
		t.Fatalf("completed preferences = %+v", cfg.Preferences)
	}
}

func TestValidateViewPreferences(t *testing.T) {
	cfg := Default(0)
	cfg.Preferences.DefaultSource = SourceDoge
	cfg.Preferences.DefaultCategory = CategoryCodex
	cfg.Preferences.RestoreViewMode = RestoreViewDefault
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid view preferences rejected: %v", err)
	}
	cfg.Preferences.DefaultSource = "unknown"
	if err := Validate(cfg); err == nil {
		t.Fatal("unknown default source accepted")
	}
	cfg = Default(0)
	cfg.Preferences.VisibleCategories = []string{CategoryCodex}
	cfg.Preferences.DefaultCategory = CategoryClaude
	if err := Validate(cfg); err == nil {
		t.Fatal("hidden default category accepted")
	}
	cfg = Default(0)
	cfg.Preferences.VisibleCategories = []string{}
	if err := Validate(cfg); err == nil {
		t.Fatal("empty visible category list accepted")
	}
}

func TestDeferredStoreDoesNotCreateUntilActivated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := NewDeferredStore(path)
	cfg, err := store.LoadOrCreate(8765)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("deferred load created config file: %v", err)
	}
	cfg.Doge.AccessToken = "fake-doge-access-token"
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("deferred save created config file: %v", err)
	}
	if err := store.ActivatePersistence(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("activated store did not create config file: %v", err)
	}
}

func TestValidateRequiresCategoryScopedActiveProfiles(t *testing.T) {
	cfg := Default(0)
	cfg.Profiles = []Profile{{
		ID: "codex-one", Source: SourceCustom, Category: CategoryCodex,
		Name: "Codex One", BaseURL: "https://example.test/v1", APIKey: "sk-placeholder",
	}}
	cfg.ActiveProfiles[CategoryClaude] = "codex-one"
	if err := Validate(cfg); err == nil {
		t.Fatal("跨类别启用映射必须被拒绝")
	}
	cfg.ActiveProfiles = map[string]string{CategoryCodex: "codex-one"}
	if err := Validate(cfg); err != nil {
		t.Fatalf("有效类别启用映射被拒绝: %v", err)
	}
}

func TestDogeConnectionDefaultsAndClone(t *testing.T) {
	cfg := Default(0)
	if cfg.Doge.BaseURL != "https://api.ergouzi.life" || cfg.Doge.SyncIntervalMinutes != 3 || cfg.Doge.TokenOrder == nil {
		t.Fatalf("doge defaults = %+v", cfg.Doge)
	}
	cfg.Doge.Groups = []string{"限时优惠组"}
	cfg.Doge.Tokens = []DogeToken{{ID: 42, Group: "限时优惠组", MaskedKey: "abcd********wxyz"}}
	cfg.Doge.TokenOrder = []string{"42"}
	clone := Clone(cfg)
	clone.Preferences.VisibleCategories[0] = CategoryOther
	clone.Doge.Groups[0] = "余额低价组"
	clone.Doge.Tokens[0].Name = "changed"
	clone.Doge.TokenOrder[0] = "changed-order"
	if cfg.Preferences.VisibleCategories[0] != Categories[0] || cfg.Doge.Groups[0] != "限时优惠组" || cfg.Doge.Tokens[0].Name != "" || cfg.Doge.TokenOrder[0] != "42" {
		t.Fatal("Doge clone shares mutable slices")
	}
}

func TestValidateDogeSyncIntervals(t *testing.T) {
	cfg := Default(0)
	for _, minutes := range []int{1, 3, 5, 10, 15, 30, 60} {
		cfg.Doge.SyncIntervalMinutes = minutes
		if err := Validate(cfg); err != nil {
			t.Fatalf("interval %d rejected: %v", minutes, err)
		}
	}
	cfg.Doge.SyncIntervalMinutes = 2
	if err := Validate(cfg); err == nil {
		t.Fatal("invalid Doge interval accepted")
	}
}

// 任务通知只访问用户完整填写的 URL，禁止非 HTTP(S) 地址与 URL 内嵌认证材料。
func TestValidateTaskNotificationURL(t *testing.T) {
	notification := NormalizeTaskNotification(TaskNotification{
		Enabled:               true,
		WebhookURL:            "https://www.pushplus.plus/send?token=placeholder&title={title}&content={content}",
		IdleGraceSeconds:      5,
		RequestTimeoutSeconds: 10,
	})
	if err := ValidateTaskNotification(notification); err != nil {
		t.Fatalf("有效推送 URL 被拒绝: %v", err)
	}
	notification.WebhookURL = "ftp://notify.example.test/send"
	if err := ValidateTaskNotification(notification); err == nil {
		t.Fatal("非 HTTP(S) 通知 URL 被接受")
	}
	notification.WebhookURL = "https://token@notify.example.test/send"
	if err := ValidateTaskNotification(notification); err == nil {
		t.Fatal("URL 内嵌认证材料被接受")
	}
}

// 首次创建通知配置时六类事件默认全选；设置页提交的明确空选择则必须被保留，避免用户
// 的关闭操作在下次读取时被悄悄改写。
func TestNormalizeTaskNotificationEventSelection(t *testing.T) {
	legacy := NormalizeTaskNotification(TaskNotification{})
	if !legacy.EventsInitialized || legacy.Events != DefaultTaskNotificationEvents() {
		t.Fatalf("既有任务通知默认事件错误: %+v", legacy.Events)
	}
	explicitEmpty := NormalizeTaskNotification(TaskNotification{EventsInitialized: true})
	if explicitEmpty.Events != (TaskNotificationEvents{}) {
		t.Fatalf("明确空事件选择被覆盖: %+v", explicitEmpty.Events)
	}
}
