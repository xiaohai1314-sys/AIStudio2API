package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateProxyCredentials 验证代理 URL 接受账号密码并拒绝空用户名
func TestValidateProxyCredentials(t *testing.T) {
	for _, value := range []string{"http://user:secret@127.0.0.1:8080", "socks5://user:secret@127.0.0.1:1080"} {
		if err := ValidateProxy(value); err != nil {
			t.Errorf("ValidateProxy(%q) = %v", value, err)
		}
	}
	if err := ValidateProxy("http://:secret@127.0.0.1:8080"); err == nil {
		t.Error("空用户名被接受")
	}
}

// TestProxyLimitValidation 验证出口上限的默认值、0 表示不限制以及负数被拒绝
func TestProxyLimitValidation(t *testing.T) {
	if cfg := Default(); cfg.MaxAccountsPerProxy != 3 || cfg.MaxConcurrencyPerProxy != 3 {
		t.Fatalf("出口上限默认值 = %d, %d", cfg.MaxAccountsPerProxy, cfg.MaxConcurrencyPerProxy)
	}
	cfg := Default()
	cfg.MaxAccountsPerProxy, cfg.MaxConcurrencyPerProxy = 0, 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("0 表示不限制 = %v", err)
	}
	cfg.MaxAccountsPerProxy = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "MAX_ACCOUNTS_PER_PROXY") {
		t.Fatalf("负的账户上限 = %v", err)
	}
	cfg.MaxAccountsPerProxy, cfg.MaxConcurrencyPerProxy = 3, -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "MAX_CONCURRENCY_PER_PROXY") {
		t.Fatalf("负的并发上限 = %v", err)
	}
}

func TestRemoteListenRequiresAPIKey(t *testing.T) {
	cfg := Default()
	cfg.ListenAddr = "0.0.0.0:2048"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "PROXY_API_KEY") {
		t.Fatalf("remote listener without key = %v, want API key error", err)
	}
	cfg.ProxyAPIKey = "test-key"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("remote listener with key = %v", err)
	}
}

// TestLoadRejectsRemoteListenWithoutKey 验证加载时直接报错，而不是静默改回回环地址
func TestLoadRejectsRemoteListenWithoutKey(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "0.0.0.0:2048")
	t.Setenv("PROXY_API_KEY", "")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "PROXY_API_KEY") {
		t.Fatalf("Load() = %v, want API key error", err)
	}
	t.Setenv("PROXY_API_KEY", "test-key")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() with key = %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:2048" {
		t.Fatalf("listen address = %q, want the configured remote address", cfg.ListenAddr)
	}
}

func TestParseTrustedProxyCIDRs(t *testing.T) {
	prefixes, err := ParseTrustedProxyCIDRs("127.0.0.1/32, 10.0.0.0/8,127.0.0.1/32")
	if err != nil || len(prefixes) != 2 {
		t.Fatalf("prefixes=%v err=%v", prefixes, err)
	}
	if _, err := ParseTrustedProxyCIDRs("10.0.0.1/8"); err == nil {
		t.Fatal("unmasked prefix accepted")
	}
}

func TestPublicRateLimitValidation(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default rate limit = %v", err)
	}
	cfg.PublicRateBurst = cfg.PublicRateLimit + 1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "PUBLIC_RATE_BURST") {
		t.Fatalf("invalid burst = %v, want burst error", err)
	}
}

// TestAuthFailurePauseDefault 验证认证失败默认暂停账户等待人工检查
func TestAuthFailurePauseDefault(t *testing.T) {
	if !Default().AuthFailurePause {
		t.Fatal("AUTH_FAILURE_PAUSE 默认应为 true")
	}
}
