package config

import "testing"

// TestParseAPIKeyRateLimits 验证每 key 速率解析、跳过未配置 key 与错误格式
func TestParseAPIKeyRateLimits(t *testing.T) {
	limits, err := ParseAPIKeyRateLimits("key-a:10, key-b:20,unknown:30", "key-a,key-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 2 || limits["key-a"].PerMinute != 10 || limits["key-b"].PerMinute != 20 {
		t.Fatalf("limits = %+v", limits)
	}
	limits, err = ParseAPIKeyRateLimits("key-a:10\nkey-b:20\n", "key-a,key-b")
	if err != nil || len(limits) != 2 || limits["key-b"].PerMinute != 20 {
		t.Fatalf("换行分隔 limits = %+v, %v", limits, err)
	}
	for _, value := range []string{"key-a", "key-a:0", "key-a:-1", ":10"} {
		if _, err := ParseAPIKeyRateLimits(value, "key-a"); err == nil {
			t.Fatalf("%q 应当被拒绝", value)
		}
	}
	if _, err := ParseAPIKeyRateLimits("key-a:10,key-a:20", "key-a"); err == nil {
		t.Fatal("重复 key 应当被拒绝")
	}
}

// TestAPIKeyRateLimitsValidation 验证配置校验使用 API_KEY_RATE_LIMITS
func TestAPIKeyRateLimitsValidation(t *testing.T) {
	cfg := Default()
	cfg.ProxyAPIKey = "key-a"
	cfg.APIKeyRateLimits = "key-a:30"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("合法配置被拒绝: %v", err)
	}
	cfg.APIKeyRateLimits = "key-a:abc"
	if err := cfg.Validate(); err == nil {
		t.Fatal("非法速率被接受")
	}
}
