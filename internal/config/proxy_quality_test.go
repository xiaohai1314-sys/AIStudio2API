package config

import "testing"

// TestProxyQuality 验证质量标签按片段解析并统一小写
func TestProxyQuality(t *testing.T) {
	cases := map[string]string{
		"socks5://user:secret@127.0.0.1:1080#Residential": "residential",
		"http://127.0.0.1:8080#mobile":                    "mobile",
		"http://127.0.0.1:8080":                           "",
		"http://127.0.0.1:8080#unknown":                   "",
	}
	for value, want := range cases {
		if got := ProxyQuality(value); got != want {
			t.Fatalf("ProxyQuality(%q) = %q, want %q", value, got, want)
		}
	}
}

// TestProxyAddress 验证比较出口时忽略质量标签
func TestProxyAddress(t *testing.T) {
	if got := ProxyAddress("socks5://user:secret@127.0.0.1:1080#residential"); got != "socks5://user:secret@127.0.0.1:1080" {
		t.Fatalf("ProxyAddress() = %q", got)
	}
	if got := ProxyAddress("http://127.0.0.1:8080"); got != "http://127.0.0.1:8080" {
		t.Fatalf("ProxyAddress() = %q", got)
	}
}

// TestValidateProxyQualityFragment 验证代理片段只接受已知质量标签
func TestValidateProxyQualityFragment(t *testing.T) {
	if err := ValidateProxy("socks5://user:secret@127.0.0.1:1080#residential"); err != nil {
		t.Fatalf("已知质量标签被拒绝: %v", err)
	}
	if err := ValidateProxy("socks5://user:secret@127.0.0.1:1080#bogus"); err == nil {
		t.Fatal("未知质量标签被接受")
	}
	if err := ValidateProxy("http://127.0.0.1:8080/path#residential"); err == nil {
		t.Fatal("带路径的代理被接受")
	}
}

// TestAccountRateValidation 验证限速配置的取值边界
func TestAccountRateValidation(t *testing.T) {
	cfg := Default()
	cfg.AccountRateLimit = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("负数请求上限被接受")
	}
	cfg = Default()
	cfg.AccountRateJitter = 91
	if err := cfg.Validate(); err == nil {
		t.Fatal("超过 90 的抖动被接受")
	}
	cfg = Default()
	cfg.AccountRateLimit = 0
	cfg.AccountRateJitter = 90
	if err := cfg.Validate(); err != nil {
		t.Fatalf("合法限速配置被拒绝: %v", err)
	}
}
