package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
	"github.com/Mag1cFall/AIStudio2API/internal/config"
)

// TestAdminConfig 验证凭据保存、密码保留与管理重启标记
func TestAdminConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	cfg := config.Default()
	cfg.AdminAuthEnabled = true
	cfg.AdminUsername = "operator"
	cfg.AdminPassword = "test-password"
	cfg.EncryptCredentials = true
	cfg.PProfAddr = "127.0.0.1:6060"
	cfg.GeoIPDBPath = "custom-geoip.mmdb"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	// .env.example 显式写了 AUTH_FAILURE_PAUSE=true，这里模拟同样的文件
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("AUTH_FAILURE_PAUSE=true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := aistudio.NewAccountPool(nil, 1)
	admin := &runtimeAdmin{configPath: path, requests: newRequestRegistry(ctx), pool: pool, service: &trackedService{pool: pool}}
	value, err := admin.RuntimeConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "test-password") || !value.AdminPasswordSet || !value.BuildNativeNonstream {
		t.Fatalf("config=%s", raw)
	}
	value.Proxy = "http://127.0.0.1:1234"
	value.AutoStart = true
	if _, err := admin.UpdateRuntimeConfig(ctx, value); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil || saved.AdminPassword != cfg.AdminPassword {
		t.Fatalf("password changed: %v", err)
	}
	if !saved.AutoStart {
		t.Fatal("auto_start was not saved")
	}
	if !saved.AuthFailurePause || !saved.EncryptCredentials || saved.PProfAddr != cfg.PProfAddr || saved.GeoIPDBPath != cfg.GeoIPDBPath {
		t.Fatalf("environment-only config changed: auth=%v encrypt=%v pprof=%q geoip=%q", saved.AuthFailurePause, saved.EncryptCredentials, saved.PProfAddr, saved.GeoIPDBPath)
	}
	manager := &runtimeManager{activeManagement: cfg}
	password := "new-test-password"
	value.AdminPassword = &password
	value, err = admin.UpdateRuntimeConfig(ctx, value)
	if err != nil {
		t.Fatal(err)
	}
	decorated := manager.decorateRuntimeConfig(value, cfg)
	if !decorated.ManagementRestartRequired || decorated.AdminPassword != nil {
		t.Fatalf("restart=%+v", decorated)
	}
	value.AdminPassword = new(string)
	_, err = admin.UpdateRuntimeConfig(ctx, value)
	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) || status.HTTPStatus() != http.StatusBadRequest {
		t.Fatalf("empty enabled password error = %v", err)
	}
	value.AdminAuthEnabled = false
	if _, err := admin.UpdateRuntimeConfig(ctx, value); err != nil {
		t.Fatal(err)
	}
}
