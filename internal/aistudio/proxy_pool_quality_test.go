package aistudio

import (
	"errors"
	"testing"
)

// newQualityTestPool 创建指定账户数的账户池
func newQualityTestPool(count int) (*AccountPool, []*Account) {
	accounts := make([]*Account, 0, count)
	for index := range count {
		accounts = append(accounts, &Account{
			ID:     string(rune('a'+index)) + "@example.com",
			Config: AccountConfig{Enabled: true}, State: AccountReady,
		})
	}
	return NewAccountPool(accounts, 2), accounts
}

// TestProxyPoolPrefersHigherQuality 验证分配优先选择出口质量更高的代理
func TestProxyPoolPrefersHigherQuality(t *testing.T) {
	pool, accounts := newQualityTestPool(2)
	proxies := NewProxyPool(pool, []string{
		"http://dc.example:8080#datacenter",
		"http://res.example:8080#residential",
	})
	if _, err := proxies.Assign(); err != nil {
		t.Fatalf("Assign() = %v", err)
	}
	for _, account := range accounts {
		if got := proxies.Binding(account.ID); got != "http://res.example:8080#residential" {
			t.Fatalf("账户 %s 绑定 %q，应优先使用住宅代理", account.ID, got)
		}
	}
}

// TestProxyPoolBalancesWithinQuality 验证同一质量等级内按绑定数均衡分配
func TestProxyPoolBalancesWithinQuality(t *testing.T) {
	pool, accounts := newQualityTestPool(4)
	proxies := NewProxyPool(pool, []string{
		"http://a.example:8080#residential",
		"http://b.example:8080#residential",
	})
	if _, err := proxies.Assign(); err != nil {
		t.Fatalf("Assign() = %v", err)
	}
	counts := map[string]int{}
	for _, account := range accounts {
		counts[proxies.Binding(account.ID)]++
	}
	if counts["http://a.example:8080#residential"] != 2 || counts["http://b.example:8080#residential"] != 2 {
		t.Fatalf("同质量代理的绑定数 = %v, want 2/2", counts)
	}
}

// TestProxyPoolFallsBackWhenHigherQualityUnavailable 验证高质量代理不可用后改绑到次优代理
func TestProxyPoolFallsBackWhenHigherQualityUnavailable(t *testing.T) {
	pool, accounts := newQualityTestPool(2)
	proxies := NewProxyPool(pool, []string{
		"http://dc.example:8080#datacenter",
		"http://res.example:8080#residential",
	})
	if _, err := proxies.Assign(); err != nil {
		t.Fatalf("Assign() = %v", err)
	}
	for range proxyFailureThreshold {
		if _, err := proxies.ReportFailure("http://res.example:8080#residential", errors.New("boom")); err != nil {
			t.Fatalf("ReportFailure() = %v", err)
		}
	}
	for _, account := range accounts {
		if got := proxies.Binding(account.ID); got != "http://dc.example:8080#datacenter" {
			t.Fatalf("账户 %s 仍绑定不可用的代理 %q", account.ID, got)
		}
	}
}

// TestProxyStatusReportsQuality 验证代理状态带出出口质量标签
func TestProxyStatusReportsQuality(t *testing.T) {
	pool, _ := newQualityTestPool(1)
	proxies := NewProxyPool(pool, []string{"socks5://user:secret@127.0.0.1:1080#mobile"})
	statuses := proxies.Status()
	if len(statuses) != 1 || statuses[0].Quality != "mobile" {
		t.Fatalf("代理状态 = %+v, want quality mobile", statuses)
	}
}
