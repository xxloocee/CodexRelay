/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 运行时数据目录迁移回归测试
 * @File          : Runtime 数据目录迁移测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package relay

import (
	"os"
	"path/filepath"
	"testing"

	"codexrelay/internal/config"
	"codexrelay/internal/usage"
)

func TestRequestObservationSurvivesOnlyUnchangedConnection(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Default(18765)
	cfg.ActiveProfiles[config.CategoryCodex] = "test"
	cfg.Profiles = []config.Profile{{ID: "test", Source: config.SourceCustom, Category: config.CategoryCodex, Name: "Test", BaseURL: "https://example.invalid/v1", APIKey: "synthetic-key"}}
	usageStore, err := usage.NewStore(filepath.Join(directory, "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(config.NewStore(filepath.Join(directory, "config.json")), usageStore, cfg)
	if err != nil {
		t.Fatal(err)
	}
	original := runtime.State().Active[config.CategoryCodex].RequestObservation
	original.LastRequestAt.Store(123)
	original.RequestDiagnosticLogged.Store(true)
	if _, err := runtime.UpdateConfig(func(next *config.AppConfig) error {
		next.Profiles[0].Name = "Renamed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	retained := runtime.State().Active[config.CategoryCodex].RequestObservation
	if retained != original || retained.LastRequestAt.Load() != 123 || !retained.RequestDiagnosticLogged.Load() {
		t.Fatal("unrelated update lost request observation")
	}
	if _, err := runtime.UpdateConfig(func(next *config.AppConfig) error {
		next.Profiles[0].APIKey = "another-synthetic-key"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changed := runtime.State().Active[config.CategoryCodex].RequestObservation
	if changed == original || changed.LastRequestAt.Load() != 0 || changed.RequestDiagnosticLogged.Load() {
		t.Fatal("changed connection retained stale request observation")
	}
}

func TestRuntimeMigrateDataDirectorySwitchesStores(t *testing.T) {
	oldDirectory := t.TempDir()
	newDirectory := filepath.Join(t.TempDir(), "new-data")
	configStore := config.NewStore(filepath.Join(oldDirectory, "config.json"))
	cfg := config.Default(18765)
	if err := configStore.Save(cfg); err != nil {
		t.Fatal(err)
	}
	usageStore, err := usage.NewStore(filepath.Join(oldDirectory, "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(configStore, usageStore, cfg)
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	previous, err := runtime.MigrateDataDirectory(newDirectory, func() error {
		committed = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if previous != oldDirectory || !committed || runtime.DataDirectory() != newDirectory {
		t.Fatalf("migration state previous=%q committed=%v current=%q", previous, committed, runtime.DataDirectory())
	}
	for _, name := range []string{"config.json", "usage.json"} {
		if _, err := os.Stat(filepath.Join(newDirectory, name)); err != nil {
			t.Fatalf("migrated %s missing: %v", name, err)
		}
	}
	if _, err := runtime.UpdateConfig(func(next *config.AppConfig) error {
		next.ProxyPort = 18766
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := configStore.LoadOrCreate(0)
	if err != nil || loaded.ProxyPort != 18766 {
		t.Fatalf("updated config was not written to new path: %+v err=%v", loaded, err)
	}
}

func TestRuntimeMigrateDataDirectoryRejectsExistingTargetFile(t *testing.T) {
	oldDirectory := t.TempDir()
	newDirectory := t.TempDir()
	configStore := config.NewStore(filepath.Join(oldDirectory, "config.json"))
	cfg := config.Default(18765)
	if err := configStore.Save(cfg); err != nil {
		t.Fatal(err)
	}
	usageStore, err := usage.NewStore(filepath.Join(oldDirectory, "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(configStore, usageStore, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDirectory, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.MigrateDataDirectory(newDirectory, nil); err == nil {
		t.Fatal("existing target config should reject migration")
	}
	if runtime.DataDirectory() != oldDirectory {
		t.Fatalf("failed migration changed runtime path to %q", runtime.DataDirectory())
	}
}
