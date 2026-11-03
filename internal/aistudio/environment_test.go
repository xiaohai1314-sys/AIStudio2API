package aistudio

import "testing"

// TestBindEnvironmentDetectsDrift 验证认证环境绑定后代理或指纹变化会被识别
func TestBindEnvironmentDetectsDrift(t *testing.T) {
	account := &Account{ID: "user@example.com", Config: AccountConfig{Enabled: true}, State: AccountReady}
	pool := NewAccountPool([]*Account{account}, 1)
	if reason := pool.EnvironmentDrift(account.ID, "http://proxy.example:8080", "digest-a"); reason != "" {
		t.Fatalf("未绑定时不应报告变化: %q", reason)
	}
	if err := pool.BindEnvironment(account.ID, "http://proxy.example:8080#residential", "digest-a"); err != nil {
		t.Fatalf("BindEnvironment() = %v", err)
	}
	if reason := pool.EnvironmentDrift(account.ID, "http://proxy.example:8080", "digest-a"); reason != "" {
		t.Fatalf("质量标签变化不应报告漂移: %q", reason)
	}
	if reason := pool.EnvironmentDrift(account.ID, "http://other.example:8080", "digest-a"); reason == "" {
		t.Fatal("出口代理变化未被识别")
	}
	if reason := pool.EnvironmentDrift(account.ID, "http://proxy.example:8080", "digest-b"); reason == "" {
		t.Fatal("指纹变化未被识别")
	}
}

// TestBindEnvironmentIgnoresEmptyValues 验证缺少代理或指纹时不写入绑定
func TestBindEnvironmentIgnoresEmptyValues(t *testing.T) {
	account := &Account{ID: "user@example.com", Config: AccountConfig{Enabled: true}, State: AccountReady}
	pool := NewAccountPool([]*Account{account}, 1)
	if err := pool.BindEnvironment(account.ID, "", "digest"); err != nil {
		t.Fatalf("BindEnvironment() = %v", err)
	}
	if reason := pool.EnvironmentDrift(account.ID, "http://proxy.example:8080", "other"); reason != "" {
		t.Fatalf("空代理不应建立绑定: %q", reason)
	}
}
