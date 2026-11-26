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
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
	"github.com/Mag1cFall/AIStudio2API/internal/config"
)

type stubStateService struct {
	managedService
	state string
}

func (s *stubStateService) State() string { return s.state }

// TestAdminConfig 验证凭据保存、密码保留、热加载键不触发重启与仅必要键触发重启标记
func TestAdminConfig(t *testing.T) {
	defer aistudio.SetUnaryRPCTimeout(config.Default().UnaryRPCTimeout)
	path := filepath.Join(t.TempDir(), ".env")
	cfg := config.Default()
	cfg.AdminAuthEnabled = true
	cfg.AdminUsername = "operator"
	cfg.AdminPassword = "test-password"
	// 非 Windows 的凭据加密需要数据目录之外的密钥。本测试保持关闭，只确认保存设置不会改掉这项。
	cfg.EncryptCredentials = false
	cfg.CredentialsKeyFile = "/run/secrets/aistudio-credentials"
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
	requests := newRequestRegistry()
	admin := &runtimeAdmin{configPath: path, requests: requests, pool: pool, service: &trackedService{pool: pool}}
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
	if value.AccountSwitchInterval != config.AccountSwitchIntervalOff || value.MaxQuotaSwitches != config.MaxQuotaSwitchesOff {
		t.Fatalf("default switch policy = interval:%q quota:%q", value.AccountSwitchInterval, value.MaxQuotaSwitches)
	}
	value.Proxy = "http://127.0.0.1:1234"
	value.AutoStart = true
	value.UnaryRPCTimeout = "37s"
	value.AccountSwitchInterval = "5"
	value.MaxQuotaSwitches = "1"
	if _, err := admin.UpdateRuntimeConfig(ctx, value); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil || saved.AdminPassword != cfg.AdminPassword {
		t.Fatalf("password changed: %v", err)
	}
	if saved.UnaryRPCTimeout != 37*time.Second {
		t.Fatalf("unary rpc timeout = %s, want 37s", saved.UnaryRPCTimeout)
	}
	if !saved.AutoStart || saved.AccountSwitchInterval != "5" || saved.MaxQuotaSwitches != "1" {
		t.Fatalf("saved fields mismatch: auto_start=%v interval=%q quota=%q", saved.AutoStart, saved.AccountSwitchInterval, saved.MaxQuotaSwitches)
	}
	if !saved.AuthFailurePause || saved.EncryptCredentials != cfg.EncryptCredentials || saved.CredentialsKeyFile != cfg.CredentialsKeyFile || saved.PProfAddr != cfg.PProfAddr || saved.GeoIPDBPath != cfg.GeoIPDBPath {
		t.Fatalf("environment-only config changed: auth=%v encrypt=%v keyfile=%q pprof=%q geoip=%q", saved.AuthFailurePause, saved.EncryptCredentials, saved.CredentialsKeyFile, saved.PProfAddr, saved.GeoIPDBPath)
	}
	stateSvc := &stubStateService{state: "RUNNING"}
	requests.start(aistudio.GenerateRequest{ID: "req-active", Model: "gemini-flash-latest"}, func() {})
	manager := &runtimeManager{
		activeManagement: cfg,
		requests:         requests,
		current:          &runtimeGeneration{service: stateSvc, config: cfg},
	}
	password := "new-test-password"
	value.AdminPassword = &password
	value.APIKey = "new-hot-api-key"
	value.BuildNativeNonstream = false
	value.YouTubeLinks = false
	value.StripThoughtSignatures = true
	value.ContinuationRewrite = false
	value.ContinuationPrompt = "custom prompt"
	value.RequestTimeout = "90s"
	value.InitTimeout = "45s"
	value.FirstEventTimeout = "30"
	value.FirstEventHedge = "10"
	value.FirstEventHedgeModels = "gemini-2.5-pro"
	value.UpstreamChannels = []string{"playground", "build"}
	value.DiscoverHiddenModels = true
	value, err = admin.UpdateRuntimeConfig(ctx, value)
	if err != nil {
		t.Fatal(err)
	}
	decorated := manager.decorateRuntimeConfig(value, cfg)
	if decorated.ManagementRestartRequired || decorated.ServiceRestartRequired || decorated.AdminPassword != nil || decorated.ActiveRequests != 1 {
		t.Fatalf("hot reload fields should not require restart and should report active_requests=1: %+v", decorated)
	}
	// 仅修改监听地址或预览监听地址才触发管理进程重启
	value.ListenAddr = "127.0.0.1:2999"
	decorated = manager.decorateRuntimeConfig(value, cfg)
	if !decorated.ManagementRestartRequired {
		t.Fatalf("listen_addr change should require management restart: %+v", decorated)
	}
	value.ListenAddr = cfg.ListenAddr
	value.PreviewListenAddr = "127.0.0.1:3050"
	decorated = manager.decorateRuntimeConfig(value, cfg)
	if !decorated.ManagementRestartRequired {
		t.Fatalf("preview_listen_addr change should require management restart: %+v", decorated)
	}
	value.PreviewListenAddr = cfg.PreviewListenAddr
	// 修改 WAA_BACKEND / AISTUDIO_AUTH_STATES 仅在生成服务运行时触发重启标记，已停止时不触发
	value.WAABackend = config.WAABackendCamoufox
	stateSvc.state = "STOPPED"
	if got := manager.decorateRuntimeConfig(value, cfg); got.ServiceRestartRequired {
		t.Fatalf("stopped service should not require restart: %+v", got)
	}
	stateSvc.state = "RUNNING"
	if got := manager.decorateRuntimeConfig(value, cfg); !got.ServiceRestartRequired {
		t.Fatalf("running service should require restart on WAA_BACKEND change: %+v", got)
	}
	value.WAABackend = cfg.WAABackend
	value.AuthStates = "auth-other"
	if got := manager.decorateRuntimeConfig(value, cfg); !got.ServiceRestartRequired {
		t.Fatalf("running service should require restart on AISTUDIO_AUTH_STATES change: %+v", got)
	}
	value.AuthStates = cfg.AuthStates
	// 非法换号间隔与额度换号上限返回 400
	badInterval := value
	badInterval.AccountSwitchInterval = "0"
	if _, err := admin.UpdateRuntimeConfig(ctx, badInterval); err == nil {
		t.Fatal("ACCOUNT_SWITCH_INTERVAL=0 should be rejected")
	}
	badQuota := value
	badQuota.MaxQuotaSwitches = "-1"
	if _, err := admin.UpdateRuntimeConfig(ctx, badQuota); err == nil {
		t.Fatal("MAX_QUOTA_SWITCHES=-1 should be rejected")
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
