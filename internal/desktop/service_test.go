/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 桌面绑定服务回归测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/relay"
	"codexrelay/internal/usage"
)

func TestReorderProfilesPersistsValidatedOrder(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	store := config.NewStore(configPath)
	cfg := config.Default(18765)
	cfg.Profiles = []config.Profile{
		{ID: "one", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "One", BaseURL: "https://one.example/v1", APIKey: "sk-one"},
		{ID: "two", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "Two", BaseURL: "https://two.example/v1", APIKey: "sk-two"},
		{ID: "three", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "Three", BaseURL: "https://three.example/v1", APIKey: "sk-three"},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	if err := service.ReorderProfiles([]string{"three", "one", "two"}); err != nil {
		t.Fatal(err)
	}
	state := runtime.State()
	if state.Config.Profiles[0].ID != "three" || state.Config.Profiles[2].ID != "two" {
		t.Fatalf("runtime order = %+v", state.Config.Profiles)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted config.AppConfig
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Profiles[0].ID != "three" || persisted.Profiles[1].ID != "one" || persisted.Profiles[2].ID != "two" {
		t.Fatalf("persisted order = %+v", persisted.Profiles)
	}
	if err := service.ReorderProfiles([]string{"three", "three", "two"}); err == nil {
		t.Fatal("duplicate profile order should fail")
	}
}

func TestReorderFailoverProfilesPersistsOrderAcrossSources(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	store := config.NewStore(configPath)
	cfg := config.Default(18765)
	cfg.Profiles = []config.Profile{
		{ID: "custom-first", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "自定义测试", BaseURL: "https://custom-first.example/v1", APIKey: "sk-custom-first"},
		{ID: "doge-middle", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "Codex 低价组", BaseURL: "https://doge.example/v1", APIKey: "sk-doge", RemoteTokenID: 42},
		{ID: "custom-last", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "自定义备用", BaseURL: "https://custom-last.example/v1", APIKey: "sk-custom-last"},
		{ID: "claude-other", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "Claude 线路", BaseURL: "https://claude.example/v1", APIKey: "sk-claude"},
	}
	cfg.FailoverOrder = map[string][]string{
		config.CategoryCodex:  {"custom-first", "doge-middle", "custom-last"},
		config.CategoryClaude: {"claude-other"},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	if err := service.ReorderFailoverProfiles(config.CategoryCodex, []string{"doge-middle", "custom-first"}); err != nil {
		t.Fatal(err)
	}
	want := "doge-middle,custom-first,custom-last"
	state := runtime.State()
	if got := strings.Join(state.Config.FailoverOrder[config.CategoryCodex], ","); got != want {
		t.Fatalf("runtime failover order = %q, want %q", got, want)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted config.AppConfig
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(persisted.FailoverOrder[config.CategoryCodex], ","); got != want {
		t.Fatalf("persisted failover order = %q, want %q", got, want)
	}
	if got := strings.Join(persisted.FailoverOrder[config.CategoryClaude], ","); got != "claude-other" {
		t.Fatalf("other category order changed = %q", got)
	}
	if err := service.ReorderFailoverProfiles(config.CategoryCodex, []string{"claude-other"}); err == nil {
		t.Fatal("cross-category profile order should fail")
	}
}

func TestSaveProfileReturnsPlaintextKeyAndNote(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	input := ProfileInput{Source: config.SourceCustom, Category: config.CategoryCodex, Name: "One", BaseURL: "https://example.test/v1", APIKey: "sk-example", Note: "稳定线路"}
	if err := service.SaveProfile(input); err != nil {
		t.Fatal(err)
	}
	state := service.GetState()
	if len(state.Profiles) != 1 || state.Profiles[0].APIKey != "sk-example" || state.Profiles[0].Note != "稳定线路" {
		t.Fatalf("profiles = %+v", state.Profiles)
	}
}

func TestSaveProfileKeepsDogeKeyImmutable(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Profiles = []config.Profile{{
		ID: "doge-profile", Source: config.SourceDoge, Category: config.CategoryCodex,
		Name: "原名称", BaseURL: "https://example.test/v1", APIKey: "sk-original", RemoteTokenID: 42,
	}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)

	if err := service.SaveProfile(ProfileInput{
		ID: "doge-profile", Source: config.SourceDoge, Category: config.CategoryCodex,
		Name: "修改名称", BaseURL: "https://example.test/v1", APIKey: "sk-changed",
	}); err == nil {
		t.Fatal("changing a doge API key should fail")
	}
	if err := service.SaveProfile(ProfileInput{
		ID: "doge-profile", Source: config.SourceDoge, Category: config.CategoryCodex,
		Name: "修改名称", BaseURL: "https://example.test/v1", APIKey: "sk-original", Note: "可编辑备注",
	}); err != nil {
		t.Fatalf("saving other doge fields = %v", err)
	}

	profile := runtime.State().Config.Profiles[0]
	if profile.Name != "修改名称" || profile.Note != "可编辑备注" || profile.APIKey != "sk-original" {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestUnbindDogeClearsAccountSnapshot(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Doge.AccessToken = "fake-doge-access-token"
	cfg.Doge.Account = config.DogeAccount{ID: 7, Quota: 1000000}
	cfg.Doge.Subscriptions = []config.DogeSubscription{{ID: 9, Status: "active", AmountTotal: 2000000}}
	cfg.Doge.Topup = config.DogeTopupInfo{EnableRedemption: true, TopupLink: "https://example.test/buy"}
	cfg.Doge.User = map[string]any{"username": "fake-user"}
	cfg.Doge.Groups = []string{"可用分组"}
	cfg.Doge.Tokens = []config.DogeToken{{ID: 41, Status: 1, Name: "二狗子令牌", Category: config.CategoryCodex, Key: "sk-doge", Group: "可用分组"}}
	cfg.Profiles = []config.Profile{
		{ID: "doge-profile", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "二狗子令牌", BaseURL: "https://doge.example/v1", APIKey: "sk-doge", RemoteTokenID: 41},
		{ID: "custom-profile", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "自定义令牌", BaseURL: "https://custom.example/v1", APIKey: "sk-custom"},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-profile", config.CategoryClaude: "custom-profile"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-profile"}, config.CategoryClaude: {"custom-profile"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	if err := service.UnbindDoge(); err != nil {
		t.Fatal(err)
	}
	doge := runtime.State().Config.Doge
	if doge.AccessToken != "" || doge.Account.Quota != 0 || len(doge.Subscriptions) != 0 || doge.Topup.EnableRedemption || doge.Topup.TopupLink != "" || doge.User != nil {
		t.Fatalf("unbound doge snapshot = %+v", doge)
	}
	state := runtime.State()
	if len(state.Config.Profiles) != 1 || state.Config.Profiles[0].ID != "custom-profile" {
		t.Fatalf("unbind retained doge profiles = %+v", state.Config.Profiles)
	}
	if state.Config.ActiveProfiles[config.CategoryCodex] != "" || state.Active[config.CategoryCodex] != nil {
		t.Fatalf("unbind retained doge route = config:%v active:%v", state.Config.ActiveProfiles, state.Active)
	}
	if state.Config.ActiveProfiles[config.CategoryClaude] != "custom-profile" || state.Active[config.CategoryClaude] == nil {
		t.Fatalf("unbind changed custom route = config:%v active:%v", state.Config.ActiveProfiles, state.Active)
	}
	if len(state.Config.FailoverOrder[config.CategoryCodex]) != 0 {
		t.Fatalf("unbind retained doge failover order = %v", state.Config.FailoverOrder)
	}
}

func TestActivateProfileRejectsDogeProfileMissingFromDirectory(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Profiles = []config.Profile{{
		ID: "stale-doge", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "目录已删除令牌",
		BaseURL: "https://doge.example/v1", APIKey: "sk-stale", RemoteTokenID: 41,
	}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	service := NewDesktopService(newTestRuntime(t, directory, store, cfg))
	if err := service.ActivateProfile("stale-doge"); err == nil || !strings.Contains(err.Error(), "最新目录") {
		t.Fatalf("missing directory profile activation error = %v", err)
	}
}

func TestCompleteOnboardingClearsRuntimeFlag(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	service.setNeedsOnboarding(true)
	if !service.GetState().NeedsOnboarding {
		t.Fatal("onboarding flag should be visible before completion")
	}
	if err := service.CompleteOnboarding(); err != nil {
		t.Fatal(err)
	}
	if service.GetState().NeedsOnboarding {
		t.Fatal("onboarding flag should be cleared after completion")
	}
}

func TestCompleteOnboardingActivatesDeferredPortableStores(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	usagePath := filepath.Join(directory, "usage.json")
	store := config.NewDeferredStore(configPath)
	cfg, err := store.LoadOrCreate(18765)
	if err != nil {
		t.Fatal(err)
	}
	usageStore, err := usage.NewDeferredStore(usagePath)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := relay.New(store, usageStore, cfg)
	if err != nil {
		t.Fatal(err)
	}
	service := NewDesktopService(runtime)
	service.setNeedsOnboarding(true)
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("deferred config exists before completion: %v", err)
	}
	if _, err := os.Stat(usagePath); !os.IsNotExist(err) {
		t.Fatalf("deferred usage exists before completion: %v", err)
	}
	if err := service.CompleteOnboarding(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config was not persisted after completion: %v", err)
	}
	if _, err := os.Stat(usagePath); err != nil {
		t.Fatalf("usage was not persisted after completion: %v", err)
	}
}

func TestSetProxyPortUpdatesRuntimeAndPersistsConfig(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	store := config.NewStore(configPath)
	cfg := config.Default(18765)
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	if err := service.SetProxyPort(18766); err != nil {
		t.Fatal(err)
	}
	if got := runtime.State().Config.ProxyPort; got != 18766 {
		t.Fatalf("runtime proxy port = %d", got)
	}
	persisted, err := store.LoadOrCreate(0)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProxyPort != 18766 {
		t.Fatalf("persisted proxy port = %d", persisted.ProxyPort)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port
	if err := service.SetProxyPort(occupiedPort); err == nil {
		t.Fatal("occupied proxy port should fail")
	}
	if got := runtime.State().Config.ProxyPort; got != 18766 {
		t.Fatalf("failed port change should keep old port, got %d", got)
	}
	for _, port := range []int{0, 65536} {
		if err := service.SetProxyPort(port); err == nil {
			t.Fatalf("invalid proxy port %d should fail", port)
		}
	}
}

func TestSetProxyListenAllInterfacesUpdatesRuntimeAndPersistsConfig(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	store := config.NewStore(configPath)
	cfg := config.Default(18765)
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	stateChanges := 0
	service.setStateChangedHandler(func() { stateChanges++ })

	if err := service.SetProxyListenAllInterfaces(true); err != nil {
		t.Fatal(err)
	}
	if !runtime.State().Config.ListenOnAllInterfaces || !service.GetState().ListenOnAllInterfaces {
		t.Fatalf("WSL2 listener setting should be enabled: config=%v state=%v", runtime.State().Config.ListenOnAllInterfaces, service.GetState().ListenOnAllInterfaces)
	}
	persisted, err := store.LoadOrCreate(0)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.ListenOnAllInterfaces {
		t.Fatal("WSL2 listener setting was not persisted")
	}
	if stateChanges == 0 {
		t.Fatal("listener setting should notify state changes")
	}

	if err := service.SetProxyListenAllInterfaces(false); err != nil {
		t.Fatal(err)
	}
	if runtime.State().Config.ListenOnAllInterfaces {
		t.Fatal("listener setting should return to loopback")
	}
}

func TestProxyListenAddress(t *testing.T) {
	if got := proxyListenAddress(8765, false); got != "127.0.0.1:8765" {
		t.Fatalf("loopback listener address = %q", got)
	}
	if got := proxyListenAddress(8765, true); got != "0.0.0.0:8765" {
		t.Fatalf("all-interface listener address = %q", got)
	}
}

func TestDogeTokenSwitchPromptUsesAvailableTokensInCurrentCategoryAndDismissesForFiveMinutes(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Doge.AccessToken = "fake-doge-access-token"
	cfg.Doge.Groups = []string{"余额低价组", "余额稳定组"}
	cfg.Doge.Tokens = []config.DogeToken{
		{ID: 41, Status: 1, Name: "当前令牌", Category: config.CategoryCodex, Key: "sk-current", Group: "余额低价组", GroupDisplayName: "余额低价组", GroupRatio: 0.02},
		{ID: 42, Status: 1, Name: "Codex 低价组", Category: config.CategoryCodex, Key: "sk-candidate", Group: "余额低价组", GroupDisplayName: "GPT低价组", GroupRatio: 0.02},
		{ID: 44, Status: 2, Name: "已禁用令牌", Category: config.CategoryCodex, Key: "sk-disabled", Group: "余额低价组", GroupDisplayName: "余额低价组", GroupRatio: 0.02},
		{ID: 43, Status: 1, Name: "其他分组", Category: config.CategoryCodex, Key: "sk-other", Group: "余额稳定组", GroupDisplayName: "余额稳定组", GroupRatio: 0.02},
	}
	cfg.Profiles = []config.Profile{
		{ID: "doge-profile", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "当前令牌", BaseURL: "https://example.test/v1", APIKey: "sk-current", RemoteTokenID: 41},
		{ID: "doge-candidate", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "Codex 低价组", BaseURL: "https://example.test/v1", APIKey: "sk-candidate", RemoteTokenID: 42},
		{ID: "doge-other", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "其他分组", BaseURL: "https://example.test/v1", APIKey: "sk-other", RemoteTokenID: 43},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-profile"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-profile", "doge-candidate", "doge-other"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	for index := 0; index < 5; index++ {
		runtime.ObserveUpstreamResult("doge-profile", config.CategoryCodex, 403, false)
	}
	prompt := service.GetState().Doge.TokenSwitch
	if prompt == nil || prompt.FailureKind != "auth" || prompt.FailureCount != 5 || len(prompt.Candidates) != 2 || prompt.Candidates[0].TokenID != 42 || prompt.Candidates[1].TokenID != 43 {
		t.Fatalf("token switch prompt = %+v", prompt)
	}
	if prompt.Candidates[0].Name != "Codex 低价组 (GPT低价组·0.02)" || prompt.Candidates[0].Group != "GPT低价组" || prompt.Candidates[0].Ratio != 0.02 {
		t.Fatalf("candidate display data = %+v", prompt.Candidates[0])
	}
	if prompt.Candidates[1].Name != "其他分组 (余额稳定组·0.02)" || prompt.Candidates[1].Group != "余额稳定组" || prompt.Candidates[1].Ratio != 0.02 {
		t.Fatalf("cross-group candidate display data = %+v", prompt.Candidates[1])
	}
	if err := service.DismissDogeTokenSwitch(prompt.Key); err != nil {
		t.Fatal(err)
	}
	if got := service.GetState().Doge.TokenSwitch; got != nil {
		t.Fatalf("dismissed prompt should be suppressed, got %+v", got)
	}
	service.switchMu.Lock()
	service.switchPrompts[prompt.Key].suppressedUntil = time.Now().Add(-time.Minute)
	service.switchMu.Unlock()
	if got := service.GetState().Doge.TokenSwitch; got != nil {
		t.Fatalf("ongoing failure should stay suppressed after five minutes, got %+v", got)
	}
}

func TestSuccessfulRequestClearsManualPromptAndNotifies(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModePrompt
	cfg.TokenSwitch.Trigger401 = true
	cfg.TokenSwitch.Trigger403 = false
	cfg.TokenSwitch.Trigger5xx = false
	cfg.TokenSwitch.TriggerNetwork = false
	cfg.Profiles = []config.Profile{
		{ID: "profile-a", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "令牌 A", BaseURL: "https://a.example/v1", APIKey: "sk-a"},
		{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "令牌 B", BaseURL: "https://b.example/v1", APIKey: "sk-b"},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "profile-a"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"profile-a", "profile-b"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
		runtime.ObserveUpstreamResult("profile-a", config.CategoryCodex, 401, false)
	}
	if prompt := service.GetState().Doge.TokenSwitch; prompt == nil {
		t.Fatal("expected manual token switch prompt")
	}
	stateChanges := 0
	service.setStateChangedHandler(func() { stateChanges++ })
	runtime.ObserveUpstreamResult("profile-a", config.CategoryCodex, 200, false)
	if stateChanges == 0 {
		t.Fatal("successful recovery should notify the independent notification window")
	}
	if prompt := service.GetState().Doge.TokenSwitch; prompt != nil {
		t.Fatalf("successful recovery should clear manual prompt, got %+v", prompt)
	}
}

func TestSwitchDogeTokenUpdatesCodexClientConfiguration(t *testing.T) {
	directory := t.TempDir()
	codexDirectory := filepath.Join(directory, ".codex")
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.LocalAccessToken = "sk-local-relay"
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{ConfigDir: codexDirectory, ConfigFile: "config.toml"}
	cfg.Doge.AccessToken = "fake-doge-access-token"
	cfg.Doge.Groups = []string{"可用分组"}
	cfg.Doge.Tokens = []config.DogeToken{
		{ID: 41, Status: 1, Name: "当前令牌", Category: config.CategoryCodex, Key: "sk-current", Group: "可用分组"},
		{ID: 42, Status: 1, Name: "候选令牌", Category: config.CategoryCodex, Key: "sk-candidate", Group: "可用分组"},
	}
	cfg.Profiles = []config.Profile{
		{ID: "doge-profile", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "当前令牌", BaseURL: "https://example.test/v1", APIKey: "sk-current", RemoteTokenID: 41},
		{ID: "doge-candidate", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "候选令牌", BaseURL: "https://example.test/v1", APIKey: "sk-candidate", RemoteTokenID: 42},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-profile"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-profile", "doge-candidate"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	for index := 0; index < 5; index++ {
		runtime.ObserveUpstreamResult("doge-profile", config.CategoryCodex, 403, false)
	}
	prompt := service.GetState().Doge.TokenSwitch
	if prompt == nil {
		t.Fatal("expected token switch prompt")
	}
	if err := service.SwitchDogeToken(prompt.Key, 42); err != nil {
		t.Fatal(err)
	}
	configText, err := os.ReadFile(filepath.Join(codexDirectory, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configText), `base_url = "http://127.0.0.1:18765/codex"`) {
		t.Fatalf("Codex config.toml was not updated: %s", configText)
	}
	authText, err := os.ReadFile(filepath.Join(codexDirectory, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(authText), `"OPENAI_API_KEY": "sk-local-relay"`) {
		t.Fatalf("Codex auth.json was not updated: %s", authText)
	}
	activeID := runtime.State().Config.ActiveProfiles[config.CategoryCodex]
	activeIndex := config.FindProfileIndex(runtime.State().Config.Profiles, activeID)
	if activeIndex < 0 || runtime.State().Config.Profiles[activeIndex].RemoteTokenID != 42 {
		t.Fatalf("candidate token was not activated: %q", activeID)
	}
	service.switchMu.Lock()
	roundCount := len(service.switchRounds)
	service.switchMu.Unlock()
	if roundCount != 0 {
		t.Fatalf("manual switch must not create an automatic switch round: %d", roundCount)
	}
	for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
		runtime.ObserveUpstreamResult(activeID, config.CategoryCodex, 403, false)
	}
	prompt = service.GetState().Doge.TokenSwitch
	if prompt == nil || prompt.Stopped || len(prompt.Candidates) != 1 || prompt.Candidates[0].ProfileID != "doge-profile" {
		t.Fatalf("manual prompt should offer the previous token without automatic round state: %+v", prompt)
	}
}

func TestDogeDirectorySyncCreatesAndClearsTokenSwitchPrompt(t *testing.T) {
	previous := config.Default(18765)
	previous.Doge.AccessToken = "fake-doge-access-token"
	previous.Doge.Groups = []string{"余额低价组"}
	previous.Doge.Tokens = []config.DogeToken{
		{ID: 41, Status: 1, Name: "当前令牌", Category: config.CategoryCodex, Key: "sk-current", Group: "余额低价组", GroupDisplayName: "余额低价组", GroupRatio: 0.02},
		{ID: 42, Status: 1, Name: "备用令牌", Category: config.CategoryCodex, Key: "sk-candidate", Group: "余额低价组", GroupDisplayName: "余额低价组", GroupRatio: 0.02},
	}
	previous.Profiles = []config.Profile{
		{ID: "doge-profile", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "当前令牌", BaseURL: "https://example.test/v1", APIKey: "sk-current", RemoteTokenID: 41},
		{ID: "doge-candidate", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "备用令牌", BaseURL: "https://example.test/v1", APIKey: "sk-candidate", RemoteTokenID: 42},
	}
	previous.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-profile"}
	previous.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-profile", "doge-candidate"}}

	current := previous.Doge
	current.Tokens = []config.DogeToken{
		{ID: 42, Status: 1, Name: "备用令牌", Category: config.CategoryCodex, Key: "sk-candidate", Group: "余额低价组", GroupDisplayName: "余额低价组", GroupRatio: 0.02},
	}
	context := buildDogeDirectorySwitchContext(previous, current)
	if context == nil || context.failureKind != "directory" || context.directoryReason != dogeDirectoryFailureMissing || context.token.ID != 41 {
		t.Fatalf("missing token context = %+v", context)
	}
	context.candidateProfiles = []config.Profile{previous.Profiles[1]}
	prompt := publicDogeTokenSwitchPrompt(*context)
	if prompt.Message == "" || len(prompt.Candidates) != 1 || prompt.Candidates[0].TokenID != 42 {
		t.Fatalf("missing token prompt = %+v", prompt)
	}

	current.Tokens = []config.DogeToken{
		{ID: 41, Status: 2, Name: "当前令牌", Category: config.CategoryCodex, Key: "sk-current", Group: "余额低价组", GroupDisplayName: "余额低价组", GroupRatio: 0.02},
		{ID: 42, Status: 1, Name: "备用令牌", Category: config.CategoryCodex, Key: "sk-candidate", Group: "余额低价组", GroupDisplayName: "余额低价组", GroupRatio: 0.02},
	}
	context = buildDogeDirectorySwitchContext(previous, current)
	if context == nil || context.directoryReason != dogeDirectoryFailureUnavailable {
		t.Fatalf("unavailable token context = %+v", context)
	}
	if recovered := buildDogeDirectorySwitchContext(previous, previous.Doge); recovered != nil {
		t.Fatalf("recovered token should clear directory context = %+v", recovered)
	}
}

func TestDogeDirectorySyncRemovesMissingProfileAndCompletesFailover(t *testing.T) {
	for _, mode := range []string{config.TokenSwitchModePrompt, config.TokenSwitchModeAuto} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			store := config.NewStore(filepath.Join(directory, "config.json"))
			cfg := config.Default(18765)
			cfg.TokenSwitch.Mode = mode
			cfg.TokenSwitch.Loop = false
			cfg.Doge.AccessToken = "fake-doge-access-token"
			cfg.Doge.Groups = []string{"可用分组"}
			cfg.Doge.Tokens = []config.DogeToken{
				{ID: 41, Status: 1, Name: "已删除令牌", Category: config.CategoryCodex, Key: "sk-removed", Group: "可用分组"},
				{ID: 42, Status: 1, Name: "保留令牌", Category: config.CategoryCodex, Key: "sk-kept", Group: "可用分组"},
			}
			cfg.Profiles = []config.Profile{
				{ID: "doge-removed", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "已删除令牌", BaseURL: "https://example.test/v1", APIKey: "sk-removed", RemoteTokenID: 41},
				{ID: "doge-kept", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "保留令牌", BaseURL: "https://example.test/v1", APIKey: "sk-kept", RemoteTokenID: 42},
				{ID: "custom-last", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "自定义备用", BaseURL: "https://custom.example/v1", APIKey: "sk-custom"},
			}
			cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-removed"}
			cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-removed", "doge-kept", "custom-last"}}
			cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
			if err := store.Save(cfg); err != nil {
				t.Fatal(err)
			}
			runtime := newTestRuntime(t, directory, store, cfg)
			service := NewDesktopService(runtime)
			current := cfg.Doge
			current.Tokens = []config.DogeToken{{ID: 42, Status: 1, Name: "保留令牌", Category: config.CategoryCodex, Key: "sk-kept", Group: "可用分组"}}
			contexts := buildDogeDirectorySwitchContexts(cfg, current)
			if context := contexts[config.CategoryCodex]; context == nil || len(context.candidateProfiles) != 2 || context.candidateProfiles[0].ID != "doge-kept" {
				t.Fatalf("directory candidates = %+v", context)
			}
			if err := service.saveDogeData(current, dogeAnnouncementSnapshot{}, defaultDogeBaseURL, cfg.Doge.AccessToken, cfg.Doge.TokenOrder); err != nil {
				t.Fatal(err)
			}
			state := runtime.State().Config
			if config.FindProfileIndex(state.Profiles, "doge-removed") >= 0 || config.FindProfileIndex(state.Profiles, "doge-kept") < 0 || config.FindProfileIndex(state.Profiles, "custom-last") < 0 {
				t.Fatalf("profiles after directory sync = %+v", state.Profiles)
			}
			if state.ActiveProfiles[config.CategoryCodex] != "" || strings.Join(state.FailoverOrder[config.CategoryCodex], ",") != "doge-kept,custom-last" {
				t.Fatalf("active/order after directory sync = active:%q order:%v", state.ActiveProfiles[config.CategoryCodex], state.FailoverOrder)
			}
			service.setDogeDirectorySwitchContexts(contexts)
			if mode == config.TokenSwitchModePrompt {
				prompt := service.GetState().Doge.TokenSwitches[config.CategoryCodex]
				if prompt == nil || prompt.CurrentProfileID != "doge-removed" || len(prompt.Candidates) != 2 {
					t.Fatalf("manual missing-directory prompt = %+v", prompt)
				}
				if err := service.SwitchToken(prompt.Key, "doge-kept"); err != nil {
					t.Fatal(err)
				}
			} else if notice := service.GetState().Doge.TokenSwitches[config.CategoryCodex]; notice == nil || notice.Mode != "auto" || notice.SwitchedToName == "" {
				t.Fatalf("automatic missing-directory notice = %+v", notice)
			}
			if active := runtime.State().Config.ActiveProfiles[config.CategoryCodex]; active != "doge-kept" {
				t.Fatalf("missing-directory failover active profile = %q", active)
			}
		})
	}
}

func TestDogeDirectoryRecoveryNoticeAfterAutomaticFailover(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.TokenSwitch.Loop = false
	cfg.Doge.AccessToken = "fake-doge-access-token"
	cfg.Doge.Groups = []string{"可用分组"}
	cfg.Doge.Tokens = []config.DogeToken{
		{ID: 41, Status: 1, Name: "当前令牌", Category: config.CategoryCodex, Key: "sk-current", Group: "可用分组"},
		{ID: 42, Status: 1, Name: "备用令牌", Category: config.CategoryCodex, Key: "sk-candidate", Group: "可用分组"},
	}
	cfg.Profiles = []config.Profile{
		{ID: "doge-current", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "当前令牌", BaseURL: "https://example.test/v1", APIKey: "sk-current", RemoteTokenID: 41},
		{ID: "doge-candidate", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "备用令牌", BaseURL: "https://example.test/v1", APIKey: "sk-candidate", RemoteTokenID: 42},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-current"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-current", "doge-candidate"}}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)

	unavailable := cfg.Doge
	unavailable.Tokens = append([]config.DogeToken(nil), cfg.Doge.Tokens...)
	unavailable.Tokens[0].Status = 2
	contexts := buildDogeDirectorySwitchContexts(cfg, unavailable)
	if err := service.saveDogeData(unavailable, dogeAnnouncementSnapshot{}, defaultDogeBaseURL, cfg.Doge.AccessToken, cfg.Doge.TokenOrder); err != nil {
		t.Fatal(err)
	}
	service.setDogeDirectorySwitchContexts(contexts)
	if active := runtime.State().Config.ActiveProfiles[config.CategoryCodex]; active != "doge-candidate" {
		t.Fatalf("automatic failover active profile = %q", active)
	}

	recovered := runtime.State().Config.Doge
	recovered.Tokens = append([]config.DogeToken(nil), unavailable.Tokens...)
	recovered.Tokens[0].Status = 1
	contexts = buildDogeDirectorySwitchContexts(runtime.State().Config, recovered)
	if len(contexts) != 0 {
		t.Fatalf("recovered directory should not create an active failure context: %+v", contexts)
	}
	if err := service.saveDogeData(recovered, dogeAnnouncementSnapshot{}, defaultDogeBaseURL, cfg.Doge.AccessToken, cfg.Doge.TokenOrder); err != nil {
		t.Fatal(err)
	}
	service.setDogeDirectorySwitchContexts(contexts)
	notice := service.GetState().Doge.TokenSwitches[config.CategoryCodex]
	if notice == nil || notice.FailureKind != "directory_recovered" || notice.CurrentProfileID != "doge-candidate" {
		t.Fatalf("recovery notice after automatic failover = %+v", notice)
	}
	if len(notice.Candidates) != 1 || notice.Candidates[0].ProfileID != "doge-current" {
		t.Fatalf("recovered token candidate = %+v", notice.Candidates)
	}
}

func TestMissingDirectoryPromptRecomputesModeAndSkipState(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.TokenSwitch.Loop = false
	cfg.Doge.AccessToken = "fake-doge-access-token"
	cfg.Doge.Groups = []string{"可用分组"}
	cfg.Doge.Tokens = []config.DogeToken{
		{ID: 41, Status: 1, Name: "已删除令牌", Category: config.CategoryCodex, Key: "sk-removed", Group: "可用分组"},
		{ID: 42, Status: 1, Name: "已跳过令牌", Category: config.CategoryCodex, Key: "sk-skipped", Group: "可用分组"},
	}
	cfg.Profiles = []config.Profile{
		{ID: "doge-removed", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "已删除令牌", BaseURL: "https://doge.example/v1", APIKey: "sk-removed", RemoteTokenID: 41},
		{ID: "doge-skipped", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "已跳过令牌", BaseURL: "https://doge.example/v1", APIKey: "sk-skipped", RemoteTokenID: 42, SkipAutoSwitch: true},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-removed"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-removed", "doge-skipped"}}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	current := cfg.Doge
	current.Tokens = current.Tokens[1:]
	contexts := buildDogeDirectorySwitchContexts(cfg, current)
	if err := service.saveDogeData(current, dogeAnnouncementSnapshot{}, defaultDogeBaseURL, cfg.Doge.AccessToken, cfg.Doge.TokenOrder); err != nil {
		t.Fatal(err)
	}
	service.setDogeDirectorySwitchContexts(contexts)
	if notice := service.GetState().Doge.TokenSwitches[config.CategoryCodex]; notice == nil || !notice.Stopped {
		t.Fatalf("automatic mode should stop while the only candidate is skipped: %+v", notice)
	}
	settings := runtime.State().Config.TokenSwitch
	settings.Mode = config.TokenSwitchModePrompt
	if err := service.SetTokenSwitchSettings(settings); err != nil {
		t.Fatal(err)
	}
	prompt := service.GetState().Doge.TokenSwitches[config.CategoryCodex]
	if prompt == nil || prompt.Mode != "manual" || len(prompt.Candidates) != 1 || prompt.Candidates[0].ProfileID != "doge-skipped" {
		t.Fatalf("manual prompt did not recompute skipped candidate = %+v", prompt)
	}
}

func TestMissingDirectoryRoundUsesLatestLoopSetting(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.TokenSwitch.Loop = false
	cfg.Doge.AccessToken = "fake-doge-access-token"
	cfg.Doge.Groups = []string{"可用分组"}
	cfg.Doge.Tokens = []config.DogeToken{
		{ID: 42, Status: 1, Name: "首项令牌", Category: config.CategoryCodex, Key: "sk-first", Group: "可用分组"},
		{ID: 41, Status: 1, Name: "已删除末项", Category: config.CategoryCodex, Key: "sk-removed", Group: "可用分组"},
	}
	cfg.Profiles = []config.Profile{
		{ID: "doge-first", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "首项令牌", BaseURL: "https://doge.example/v1", APIKey: "sk-first", RemoteTokenID: 42},
		{ID: "doge-removed", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "已删除末项", BaseURL: "https://doge.example/v1", APIKey: "sk-removed", RemoteTokenID: 41},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-removed"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-first", "doge-removed"}}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	current := cfg.Doge
	current.Tokens = current.Tokens[:1]
	contexts := buildDogeDirectorySwitchContexts(cfg, current)
	if err := service.saveDogeData(current, dogeAnnouncementSnapshot{}, defaultDogeBaseURL, cfg.Doge.AccessToken, cfg.Doge.TokenOrder); err != nil {
		t.Fatal(err)
	}
	service.setDogeDirectorySwitchContexts(contexts)
	if notice := service.GetState().Doge.TokenSwitches[config.CategoryCodex]; notice == nil || !notice.Stopped {
		t.Fatalf("loop-off last item should stop = %+v", notice)
	}
	settings := runtime.State().Config.TokenSwitch
	settings.Loop = true
	if err := service.SetTokenSwitchSettings(settings); err != nil {
		t.Fatal(err)
	}
	state := service.GetState()
	if state.ActiveProfiles[config.CategoryCodex] != "doge-first" {
		t.Fatalf("latest loop setting did not resume at first item = %+v", state.ActiveProfiles)
	}
	if notice := state.Doge.TokenSwitches[config.CategoryCodex]; notice == nil || notice.Stopped || notice.SwitchedToName == "" {
		t.Fatalf("resumed loop notice retained stopped state = %+v", notice)
	}
}

func TestMissingDirectoryStoppedRoundAcceptsNewProfile(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.TokenSwitch.Loop = false
	cfg.Doge.AccessToken = "fake-doge-access-token"
	cfg.Doge.Groups = []string{"可用分组"}
	cfg.Doge.Tokens = []config.DogeToken{{ID: 41, Status: 1, Name: "唯一令牌", Category: config.CategoryCodex, Key: "sk-only", Group: "可用分组"}}
	cfg.Profiles = []config.Profile{{ID: "doge-only", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "唯一令牌", BaseURL: "https://doge.example/v1", APIKey: "sk-only", RemoteTokenID: 41}}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "doge-only"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"doge-only"}}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	current := cfg.Doge
	current.Tokens = nil
	contexts := buildDogeDirectorySwitchContexts(cfg, current)
	if err := service.saveDogeData(current, dogeAnnouncementSnapshot{}, defaultDogeBaseURL, cfg.Doge.AccessToken, cfg.Doge.TokenOrder); err != nil {
		t.Fatal(err)
	}
	service.setDogeDirectorySwitchContexts(contexts)
	if notice := service.GetState().Doge.TokenSwitches[config.CategoryCodex]; notice == nil || !notice.Stopped {
		t.Fatalf("empty round should stop before adding a profile = %+v", notice)
	}
	if err := service.SaveProfile(ProfileInput{Source: config.SourceCustom, Category: config.CategoryCodex, Name: "新增备用", BaseURL: "https://custom.example/v1", APIKey: "sk-new"}); err != nil {
		t.Fatal(err)
	}
	state := service.GetState()
	if state.ActiveProfiles[config.CategoryCodex] == "" {
		t.Fatalf("new profile did not resume missing-directory round = %+v", state.ActiveProfiles)
	}
	if notice := state.Doge.TokenSwitches[config.CategoryCodex]; notice == nil || notice.Stopped || notice.SwitchedToName != "新增备用（自定义 API）" {
		t.Fatalf("new candidate notice = %+v", notice)
	}
}

func TestTokenSwitchPromptFormatsCustomCandidateName(t *testing.T) {
	prompt := publicDogeTokenSwitchPrompt(tokenSwitchContext{
		key: "doge-current|auth|401|1", failureKind: "auth", failureCount: 5, failureStatus: 401,
		profile: config.Profile{ID: "doge-current", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "Codex 低价组", RemoteTokenID: 41},
		token:   config.DogeToken{ID: 41, Name: "Codex 低价组", GroupDisplayName: "GPT低价组", GroupRatio: 0.02},
		candidateProfiles: []config.Profile{
			{ID: "custom-next", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "OpenRouter 主线路"},
		},
	})
	if prompt == nil || len(prompt.Candidates) != 1 || prompt.Candidates[0].Name != "OpenRouter 主线路（自定义 API）" {
		t.Fatalf("custom candidate display data = %+v", prompt)
	}
}

func TestAutoTokenSwitchUsesUnifiedProfileOrderAcrossSources(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.TokenSwitch.Loop = false
	cfg.TokenSwitch.Trigger401 = true
	cfg.TokenSwitch.Trigger403 = false
	cfg.TokenSwitch.Trigger5xx = false
	cfg.TokenSwitch.TriggerNetwork = false
	cfg.Profiles = []config.Profile{
		{ID: "custom-current", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "自定义当前", BaseURL: "https://current.example/v1", APIKey: "sk-current"},
		{ID: "doge-next", Source: config.SourceDoge, Category: config.CategoryCodex, Name: "二狗子下一项", BaseURL: "https://doge.example/v1", APIKey: "sk-doge", RemoteTokenID: 42},
		{ID: "custom-last", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "自定义末项", BaseURL: "https://last.example/v1", APIKey: "sk-last"},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "custom-current"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"custom-current", "doge-next", "custom-last"}}
	cfg.Doge.Groups = []string{"可用分组"}
	cfg.Doge.Tokens = []config.DogeToken{{ID: 42, Status: 1, Name: "二狗子下一项", Category: config.CategoryCodex, Key: "sk-doge", Group: "可用分组"}}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
		runtime.ObserveUpstreamResult("custom-current", config.CategoryCodex, 401, false)
	}
	state := service.GetState()
	if state.ActiveProfiles[config.CategoryCodex] != "doge-next" {
		t.Fatalf("auto switch active profile = %q, want doge-next", state.ActiveProfiles[config.CategoryCodex])
	}
	if state.Doge.TokenSwitch == nil || state.Doge.TokenSwitch.CurrentName != "自定义当前（自定义 API）" {
		t.Fatalf("custom current profile should include source in notice, got %+v", state.Doge.TokenSwitch)
	}
	if state.Doge.TokenSwitch == nil || state.Doge.TokenSwitch.Mode != "auto" || state.Doge.TokenSwitch.SwitchedToName != "二狗子下一项" {
		t.Fatalf("successful auto switch should expose independent notice, got %+v", state.Doge.TokenSwitch)
	}
	if err := service.DismissDogeTokenSwitch(state.Doge.TokenSwitch.Key); err != nil {
		t.Fatalf("dismiss automatic switch notice = %v", err)
	}
	if service.GetState().Doge.TokenSwitch != nil {
		t.Fatal("dismissed automatic switch notice should disappear")
	}
}

func TestAutoTokenSwitchStopsAfterOneAttemptPerProfileAndResetsAfterRecovery(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.TokenSwitch.Loop = true
	cfg.TokenSwitch.Trigger401 = true
	cfg.TokenSwitch.Trigger403 = false
	cfg.TokenSwitch.Trigger5xx = false
	cfg.TokenSwitch.TriggerNetwork = false
	cfg.Profiles = []config.Profile{
		{ID: "profile-a", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "令牌 A", BaseURL: "https://a.example/v1", APIKey: "sk-a"},
		{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "令牌 B", BaseURL: "https://b.example/v1", APIKey: "sk-b"},
		{ID: "profile-c", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "令牌 C", BaseURL: "https://c.example/v1", APIKey: "sk-c"},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "profile-a"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"profile-a", "profile-b", "profile-c"}}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	observeAuthFailures := func(profileID string) {
		for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
			runtime.ObserveUpstreamResult(profileID, config.CategoryCodex, 401, false)
		}
	}

	observeAuthFailures("profile-a")
	state := service.GetState()
	if state.ActiveProfiles[config.CategoryCodex] != "profile-b" || state.Doge.TokenSwitch == nil || len(state.Doge.TokenSwitch.SwitchHistory) != 1 {
		t.Fatalf("first automatic switch = active:%q prompt:%+v", state.ActiveProfiles[config.CategoryCodex], state.Doge.TokenSwitch)
	}
	observeAuthFailures("profile-b")
	state = service.GetState()
	if state.ActiveProfiles[config.CategoryCodex] != "profile-c" || state.Doge.TokenSwitch == nil || len(state.Doge.TokenSwitch.SwitchHistory) != 2 {
		t.Fatalf("second automatic switch = active:%q prompt:%+v", state.ActiveProfiles[config.CategoryCodex], state.Doge.TokenSwitch)
	}
	observeAuthFailures("profile-c")
	state = service.GetState()
	if state.ActiveProfiles[config.CategoryCodex] != "profile-c" || state.Doge.TokenSwitch == nil || !state.Doge.TokenSwitch.Stopped {
		t.Fatalf("all profiles should stop automatic switching = active:%q prompt:%+v", state.ActiveProfiles[config.CategoryCodex], state.Doge.TokenSwitch)
	}
	if len(state.Doge.TokenSwitch.SwitchHistory) != 3 || state.Doge.TokenSwitch.SwitchHistory[0].FromName == "" || state.Doge.TokenSwitch.SwitchHistory[0].SwitchedAt == "" || state.Doge.TokenSwitch.SwitchHistory[0].FailureMessage == "" {
		t.Fatalf("switch history should retain switches and the final failure = %+v", state.Doge.TokenSwitch.SwitchHistory)
	}
	finalFailure := state.Doge.TokenSwitch.SwitchHistory[2]
	if finalFailure.FromName != "令牌 C（自定义 API）" || finalFailure.ToName != "" || finalFailure.SwitchedAt == "" || !strings.Contains(finalFailure.FailureMessage, "HTTP 401") {
		t.Fatalf("final failure history = %+v", finalFailure)
	}

	if err := service.DismissDogeTokenSwitch(state.Doge.TokenSwitch.Key); err != nil {
		t.Fatalf("dismiss stopped notice = %v", err)
	}
	if service.GetState().Doge.TokenSwitch != nil {
		t.Fatal("dismissed stopped notice should not immediately reappear")
	}
	runtime.ObserveUpstreamResult("profile-c", config.CategoryCodex, 200, false)
	if service.GetState().Doge.TokenSwitch != nil {
		t.Fatal("successful request should clear the stopped switch round")
	}
	observeAuthFailures("profile-c")
	state = service.GetState()
	if state.ActiveProfiles[config.CategoryCodex] != "profile-a" || state.Doge.TokenSwitch == nil || len(state.Doge.TokenSwitch.SwitchHistory) != 1 {
		t.Fatalf("recovery should allow a new looped round = active:%q prompt:%+v", state.ActiveProfiles[config.CategoryCodex], state.Doge.TokenSwitch)
	}
}

func TestSetDogeTokenCategoriesImportsProfilesAndAppendsFailoverOrder(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Doge.BaseURL = "https://example.test"
	cfg.Doge.Tokens = []config.DogeToken{
		{ID: 41, Status: 1, Name: "待导入 Codex", Category: config.CategoryCodex, Key: "sk-token-41"},
		{ID: 42, Status: 1, Name: "待导入 Claude", Key: "sk-token-42"},
	}
	cfg.Profiles = []config.Profile{{ID: "custom-first", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "自定义首项", BaseURL: "https://custom.example/v1", APIKey: "sk-custom"}}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"custom-first"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	service := NewDesktopService(newTestRuntime(t, directory, store, cfg))
	if err := service.SetDogeTokenCategories([]DogeTokenCategoryInput{{ID: 41, Category: config.CategoryCodex}, {ID: 42, Category: config.CategoryClaude}}); err != nil {
		t.Fatalf("SetDogeTokenCategories() error = %v", err)
	}
	state := service.runtime.State().Config
	if len(state.Profiles) != 3 || state.Doge.Tokens[1].Category != config.CategoryClaude {
		t.Fatalf("imported config = profiles:%+v tokens:%+v", state.Profiles, state.Doge.Tokens)
	}
	codexOrder := state.FailoverOrder[config.CategoryCodex]
	if len(codexOrder) != 2 || codexOrder[0] != "custom-first" {
		t.Fatalf("codex failover order = %v", codexOrder)
	}
	imported := service.GetState().Doge.Tokens
	if len(imported) != 2 || !imported[0].Imported || !imported[1].Imported || imported[0].ProfileID == "" || imported[1].ProfileID == "" {
		t.Fatalf("public imported tokens = %+v", imported)
	}
}

func TestSetDogeTokenCategoriesRejectsWholeBatchWhenKeyIsIncomplete(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.Doge.Tokens = []config.DogeToken{{ID: 41, Key: "sk-full-key"}, {ID: 42, Key: "sk-ab********cd"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	service := NewDesktopService(newTestRuntime(t, directory, store, cfg))
	err := service.SetDogeTokenCategories([]DogeTokenCategoryInput{{ID: 41, Category: config.CategoryCodex}, {ID: 42, Category: config.CategoryClaude}})
	state := service.runtime.State().Config
	if err == nil || len(state.Profiles) != 0 || state.Doge.Tokens[0].Category != "" || state.Doge.Tokens[1].Category != "" {
		t.Fatalf("failed import batch must not save partial state: error=%v profiles=%+v tokens=%+v", err, state.Profiles, state.Doge.Tokens)
	}
}

func TestFailoverCandidatesRespectSkipAndLoopSettings(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.Profiles = []config.Profile{
		{ID: "profile-a", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "A", BaseURL: "https://a.example/v1", APIKey: "sk-a"},
		{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"},
		{ID: "profile-c", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "C", BaseURL: "https://c.example/v1", APIKey: "sk-c", SkipAutoSwitch: true},
	}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"profile-a", "profile-b", "profile-c"}}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "profile-b"}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	service := NewDesktopService(newTestRuntime(t, directory, store, cfg))
	if err := service.SetProfileAutoSwitch("profile-c", false); err != nil {
		t.Fatal(err)
	}
	persisted, err := store.LoadOrCreate(0)
	if err != nil || !persisted.Profiles[2].SkipAutoSwitch {
		t.Fatalf("persisted skip state = config:%+v error:%v", persisted.Profiles, err)
	}
	if candidates := service.failoverCandidates(config.CategoryCodex, "profile-b", false); len(candidates) != 0 {
		t.Fatalf("loop off should not wrap and should skip C, got %+v", candidates)
	}
	if candidates := service.failoverCandidates(config.CategoryCodex, "profile-b", true); len(candidates) != 1 || candidates[0].ID != "profile-a" {
		t.Fatalf("loop on should wrap once while skipping C, got %+v", candidates)
	}
	if err := service.updateConfig(func(cfg *config.AppConfig) error { cfg.TokenSwitch.Mode = config.TokenSwitchModePrompt; return nil }); err != nil {
		t.Fatal(err)
	}
	if candidates := service.failoverCandidates(config.CategoryCodex, "profile-b", false); len(candidates) != 1 || candidates[0].ID != "profile-c" {
		t.Fatalf("manual mode should retain skipped profiles, got %+v", candidates)
	}
	if err := service.SetProfileAutoSwitch("profile-c", true); err != nil {
		t.Fatal(err)
	}
	if service.runtime.State().Config.Profiles[2].SkipAutoSwitch {
		t.Fatal("SetProfileAutoSwitch should persist participation state")
	}
	persisted, err = store.LoadOrCreate(0)
	if err != nil || persisted.Profiles[2].SkipAutoSwitch {
		t.Fatalf("persisted participation state = config:%+v error:%v", persisted.Profiles, err)
	}
}

func TestAutomaticSwitchNoticesAreIsolatedByCategory(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.TokenSwitch.Trigger403 = false
	cfg.Profiles = []config.Profile{
		{ID: "codex-a", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "Codex A", BaseURL: "https://a.example/v1", APIKey: "sk-a"},
		{ID: "codex-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "Codex B", BaseURL: "https://b.example/v1", APIKey: "sk-b"},
		{ID: "claude-a", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "Claude A", BaseURL: "https://c.example/v1", APIKey: "sk-c"},
		{ID: "claude-b", Source: config.SourceCustom, Category: config.CategoryClaude, Name: "Claude B", BaseURL: "https://d.example/v1", APIKey: "sk-d"},
	}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"codex-a", "codex-b"}, config.CategoryClaude: {"claude-a", "claude-b"}}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "codex-a", config.CategoryClaude: "claude-a"}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	cfg.ClientConfigs[config.CategoryClaude] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
		runtime.ObserveUpstreamResult("codex-a", config.CategoryCodex, 401, false)
		runtime.ObserveUpstreamResult("claude-a", config.CategoryClaude, 401, false)
	}
	state := service.GetState()
	if len(state.Doge.TokenSwitches) != 2 || state.Doge.TokenSwitches[config.CategoryCodex] == nil || state.Doge.TokenSwitches[config.CategoryClaude] == nil {
		t.Fatalf("category notices = %+v", state.Doge.TokenSwitches)
	}
	codexNotice := state.Doge.TokenSwitches[config.CategoryCodex]
	if err := service.DismissDogeTokenSwitch(codexNotice.Key); err != nil {
		t.Fatal(err)
	}
	state = service.GetState()
	if state.Doge.TokenSwitches[config.CategoryCodex] != nil || state.Doge.TokenSwitches[config.CategoryClaude] == nil {
		t.Fatalf("dismissing Codex must not affect Claude: %+v", state.Doge.TokenSwitches)
	}
}

func TestSuccessfulRequestClearsActiveRoundButKeepsNotice(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.Profiles = []config.Profile{
		{ID: "profile-a", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "A", BaseURL: "https://a.example/v1", APIKey: "sk-a"},
		{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"},
	}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"profile-a", "profile-b"}}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "profile-a"}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
		runtime.ObserveUpstreamResult("profile-a", config.CategoryCodex, 401, false)
	}
	runtime.ObserveUpstreamResult("profile-b", config.CategoryCodex, 200, false)
	service.switchMu.Lock()
	_, roundExists := service.switchRounds[config.CategoryCodex]
	service.switchMu.Unlock()
	if roundExists || service.GetState().Doge.TokenSwitches[config.CategoryCodex] == nil {
		t.Fatalf("successful request should clear round and retain notice, round=%v notices=%+v", roundExists, service.GetState().Doge.TokenSwitches)
	}
}

func TestSwitchingToManualModeClearsAutomaticRoundAndNotice(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.Profiles = []config.Profile{
		{ID: "profile-a", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "A", BaseURL: "https://a.example/v1", APIKey: "sk-a"},
		{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"},
	}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"profile-a", "profile-b"}}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "profile-a"}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
		runtime.ObserveUpstreamResult("profile-a", config.CategoryCodex, 401, false)
	}
	if service.GetState().Doge.TokenSwitches[config.CategoryCodex] == nil {
		t.Fatal("automatic switch notice was not created")
	}
	settings := runtime.State().Config.TokenSwitch
	settings.Mode = config.TokenSwitchModePrompt
	if err := service.SetTokenSwitchSettings(settings); err != nil {
		t.Fatal(err)
	}
	service.switchMu.Lock()
	roundCount, noticeCount := len(service.switchRounds), len(service.autoSwitchNotices)
	service.switchMu.Unlock()
	if roundCount != 0 || noticeCount != 0 {
		t.Fatalf("manual mode retained automatic state: rounds=%d notices=%d", roundCount, noticeCount)
	}
	if prompt := service.GetState().Doge.TokenSwitches[config.CategoryCodex]; prompt != nil && prompt.Mode == "auto" {
		t.Fatalf("manual mode displayed an automatic notice: %+v", prompt)
	}
}

func TestSetTokenSwitchSettingsDoesNotReuseDisabled401Failures(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	cfg.TokenSwitch.Mode = config.TokenSwitchModeAuto
	cfg.TokenSwitch.Trigger401 = false
	cfg.TokenSwitch.Trigger403 = false
	cfg.TokenSwitch.Trigger5xx = false
	cfg.TokenSwitch.TriggerNetwork = false
	cfg.Profiles = []config.Profile{
		{ID: "profile-a", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "A", BaseURL: "https://a.example/v1", APIKey: "sk-a"},
		{ID: "profile-b", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "B", BaseURL: "https://b.example/v1", APIKey: "sk-b"},
	}
	cfg.ActiveProfiles = map[string]string{config.CategoryCodex: "profile-a"}
	cfg.FailoverOrder = map[string][]string{config.CategoryCodex: {"profile-a", "profile-b"}}
	cfg.ClientConfigs[config.CategoryCodex] = config.ClientConfig{SkipConfigReplacement: true}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, directory, store, cfg)
	service := NewDesktopService(runtime)
	for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
		runtime.ObserveUpstreamResult("profile-a", config.CategoryCodex, 401, false)
	}
	settings := runtime.State().Config.TokenSwitch
	settings.Trigger401 = true
	if err := service.SetTokenSwitchSettings(settings); err != nil {
		t.Fatal(err)
	}
	if state := service.GetState(); state.ActiveProfiles[config.CategoryCodex] != "profile-a" || state.Doge.TokenSwitches[config.CategoryCodex] != nil {
		t.Fatalf("re-enabling 401 reused disabled-period failures = active:%q prompt:%+v", state.ActiveProfiles[config.CategoryCodex], state.Doge.TokenSwitches[config.CategoryCodex])
	}
	for index := 0; index < config.DefaultAuthFailureThreshold; index++ {
		runtime.ObserveUpstreamResult("profile-a", config.CategoryCodex, 401, false)
	}
	if active := service.GetState().ActiveProfiles[config.CategoryCodex]; active != "profile-b" {
		t.Fatalf("new enabled-period failures did not trigger switching: %q", active)
	}
}

func TestExpiredSubscriptionsDoNotIncreaseDisplayedPackageBalance(t *testing.T) {
	directory := t.TempDir()
	store := config.NewStore(filepath.Join(directory, "config.json"))
	cfg := config.Default(18765)
	now := time.Now()
	cfg.Doge.Subscriptions = []config.DogeSubscription{
		{ID: 9, PlanTitle: "有效套餐", Status: "active", AmountTotal: 1000000, EndTime: now.Add(72 * time.Hour).Unix()},
		{ID: 10, PlanTitle: "已过期套餐", Status: "expired", AmountTotal: 2000000, EndTime: now.Add(-time.Hour).Unix()},
	}
	cfg.Doge.Notifications.SubscriptionAlertRecords = []config.DogeSubscriptionAlertRecord{{
		SubscriptionID: 10, AmountUSD: 4, ThresholdUSD: 1, State: subscriptionAlertStateExpired, NotifiedAt: now,
	}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	state := NewDesktopService(newTestRuntime(t, directory, store, cfg)).GetState().Doge
	if state.SubscriptionsUSD != 2 || len(state.Subscriptions) != 1 || state.Subscriptions[0].ID != 9 {
		t.Fatalf("displayed subscriptions = total:%v rows:%+v", state.SubscriptionsUSD, state.Subscriptions)
	}
	if len(state.Notifications.Alerts) != 1 || state.Notifications.Alerts[0].Title != "套餐已过期" {
		t.Fatalf("expired subscription alert = %+v", state.Notifications.Alerts)
	}
}

func newTestRuntime(t *testing.T, directory string, store *config.Store, cfg config.AppConfig) *relay.Runtime {
	t.Helper()
	usageStore, err := usage.NewStore(filepath.Join(directory, "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := relay.New(store, usageStore, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
