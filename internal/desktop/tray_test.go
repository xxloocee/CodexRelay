/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 系统托盘令牌筛选回归测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"testing"

	"codexrelay/internal/config"
)

func TestTrayProfileSelectableFiltersUnavailableDogeTokens(t *testing.T) {
	base := config.Profile{Source: config.SourceDoge, Category: config.CategoryCodex, RemoteTokenID: 42}
	tests := []struct {
		name    string
		profile config.Profile
		tokens  []config.DogeToken
		groups  []string
		want    bool
	}{
		{
			name:    "可用令牌保留",
			profile: base,
			tokens:  []config.DogeToken{{ID: 42, Status: 1, Key: "sk-available", Group: "余额低价组"}},
			groups:  []string{"余额低价组"},
			want:    true,
		},
		{
			name:    "禁用令牌过滤",
			profile: base,
			tokens:  []config.DogeToken{{ID: 42, Status: 2, Group: "余额低价组"}},
			groups:  []string{"余额低价组"},
			want:    false,
		},
		{
			name:    "无权限分组过滤",
			profile: base,
			tokens:  []config.DogeToken{{ID: 42, Status: 1, Group: "余额低价组"}},
			groups:  []string{"余额稳定组"},
			want:    false,
		},
		{
			name:    "目录中已消失过滤",
			profile: base,
			tokens:  []config.DogeToken{{ID: 41, Status: 1, Group: "余额低价组"}},
			groups:  []string{"余额低价组"},
			want:    false,
		},
		{
			name:    "自定义 API 保留",
			profile: config.Profile{Source: config.SourceCustom, Category: config.CategoryCodex, RemoteTokenID: 42},
			tokens:  []config.DogeToken{{ID: 42, Status: 2, Group: "余额低价组"}},
			groups:  nil,
			want:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := trayProfileSelectable(test.profile, test.tokens, test.groups); got != test.want {
				t.Fatalf("trayProfileSelectable(%+v, %+v, %v) = %v, want %v", test.profile, test.tokens, test.groups, got, test.want)
			}
		})
	}
}

func TestTrayVisibleCategorySetUsesConfiguredCategories(t *testing.T) {
	visible := trayVisibleCategorySet([]string{config.CategoryCodex, config.CategoryGrok})
	if !visible[config.CategoryCodex] || !visible[config.CategoryGrok] {
		t.Fatalf("configured categories were not retained: %#v", visible)
	}
	if visible[config.CategoryClaude] {
		t.Fatalf("hidden category was included: %#v", visible)
	}
	all := trayVisibleCategorySet(nil)
	if len(all) != len(config.Categories) {
		t.Fatalf("empty visibility should use all categories: got %d want %d", len(all), len(config.Categories))
	}
}

func TestTrayDogeTokenEntryIncludesGroupAndRequiresCompleteKey(t *testing.T) {
	token := config.DogeToken{ID: 42, Category: config.CategoryCodex, Key: "sk-full-key", Status: 1, Group: "余额低价组", GroupDisplayName: "GPT低价组", GroupRatio: 0.02}
	if !trayDogeTokenSelectable(token, []string{"GPT低价组"}) {
		t.Fatal("complete available token should be selectable")
	}
	if trayDogeTokenSelectable(token, []string{"GPT稳定组"}) {
		t.Fatal("token without permitted group should be filtered")
	}
	token.Key = "sk-UkRl**********ZYf4"
	if trayDogeTokenSelectable(token, []string{"GPT低价组"}) {
		t.Fatal("masked token should not be added as an activation entry")
	}
	if got := trayDogeTokenLabel(token.Name, token); got != "未命名令牌 (GPT低价组·0.02)" {
		t.Fatalf("tray token label = %q", got)
	}
}

func TestDogeTokenDisplayGroupUsesOnlyDisplayName(t *testing.T) {
	token := config.DogeToken{Group: "余额低价组", GroupDisplayName: "GPT低价组"}
	if got := dogeTokenDisplayGroup(token); got != "GPT低价组" {
		t.Fatalf("display group = %q, want display name", got)
	}
	token.GroupDisplayName = ""
	if got := dogeTokenDisplayGroup(token); got != "" {
		t.Fatalf("missing display name should not fall back to raw group: %q", got)
	}
}

func TestNonHomeCustomProfileNameIncludesSource(t *testing.T) {
	if got := formatNonHomeProfileName("OpenRouter 主线路", config.SourceCustom, "", 0); got != "OpenRouter 主线路（自定义 API）" {
		t.Fatalf("custom profile label = %q", got)
	}
}

func TestTrayEntriesFollowFailoverOrderAndFilterUnavailableProfiles(t *testing.T) {
	cfg := config.Default(18765)
	cfg.Profiles = []config.Profile{
		{ID: "custom-first", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "自定义首项", APIKey: "sk-a"},
		{ID: "doge-disabled", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "二狗子失效项", APIKey: "sk-b", RemoteTokenID: 42},
		{ID: "custom-last", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "自定义末项", APIKey: "sk-c"},
	}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"custom-last", "doge-disabled", "custom-first"}}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-disabled"}
	cfg.Doge.Tokens = []config.DogeToken{
		{ID: 42, Status: 2, Key: "sk-b", Group: "可用分组"},
		{ID: 99, Status: 1, Key: "sk-pending", Group: "可用分组", Category: config.CategoryCodex},
	}
	cfg.Doge.Groups = []string{"可用分组"}
	entries := trayEntriesForCategory(cfg, config.CategoryCodex)
	if len(entries) != 2 || entries[0].profileID != "custom-last" || entries[1].profileID != "custom-first" {
		t.Fatalf("tray entries = %+v", entries)
	}
	for _, entry := range entries {
		if entry.profileID == "doge-disabled" {
			t.Fatalf("disabled token remained in tray entries: %+v", entries)
		}
	}
}

func TestTrayEntriesHideCategoryWhenNoSelectableProfilesRemain(t *testing.T) {
	cfg := config.Default(18765)
	cfg.Profiles = []config.Profile{
		{ID: "doge-disabled", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "二狗子失效项", APIKey: "sk-b", RemoteTokenID: 42},
	}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-disabled"}}
	cfg.Doge.Tokens = []config.DogeToken{{ID: 42, Status: 2, Key: "sk-b", Group: "可用分组"}}
	cfg.Doge.Groups = []string{"可用分组"}
	if entries := trayEntriesForCategory(cfg, config.CategoryCodex); len(entries) != 0 {
		t.Fatalf("category with only disabled tokens should be empty: %+v", entries)
	}
	if entries := trayEntriesForCategory(cfg, config.CategoryClaude); len(entries) != 0 {
		t.Fatalf("category without profiles should be empty: %+v", entries)
	}
}
