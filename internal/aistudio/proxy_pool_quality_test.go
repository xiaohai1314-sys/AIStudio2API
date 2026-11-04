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

// TestProxyPoolCapsAccountsPerProxy 验证达到出口账户上限后改绑到其他可用代理
func TestProxyPoolCapsAccountsPerProxy(t *testing.T) {
	pool, accounts := newQualityTestPool(4)
	proxies := NewProxyPool(pool, []string{
		"http://res.example:8080#residential",
		"http://dc.example:8080#datacenter",
	})
	proxies.SetAccountLimit(2)
	if _, err := proxies.Assign(); err != nil {
		t.Fatalf("Assign() = %v", err)
	}
	counts := map[string]int{}
	for _, account := range accounts {
		counts[proxies.Binding(account.ID)]++
	}
	if counts["http://res.example:8080#residential"] != 2 || counts["http://dc.example:8080#datacenter"] != 2 {
		t.Fatalf("出口绑定数 = %v, want 2/2", counts)
	}
}

// TestProxyPoolRebalancesAfterLoweringLimit 验证调低上限后重新分配已超限的绑定
func TestProxyPoolRebalancesAfterLoweringLimit(t *testing.T) {
	pool, accounts := newQualityTestPool(4)
	proxies := NewProxyPool(pool, []string{
		"http://res.example:8080#residential",
		"http://dc.example:8080#datacenter",
	})
	if _, err := proxies.Assign(); err != nil {
		t.Fatalf("Assign() = %v", err)
	}
	proxies.SetAccountLimit(2)
	changed, err := proxies.Assign()
	if err != nil {
		t.Fatalf("Assign() = %v", err)
	}
	if len(changed) != 2 {
		t.Fatalf("改绑账户数 = %d, want 2", len(changed))
	}
	counts := map[string]int{}
	for _, account := range accounts {
		counts[proxies.Binding(account.ID)]++
	}
	if counts["http://res.example:8080#residential"] != 2 || counts["http://dc.example:8080#datacenter"] != 2 {
		t.Fatalf("出口绑定数 = %v, want 2/2", counts)
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

// TestProxyScoreTracksOutcomes 验证成功与风控命中改变健康评分
func TestProxyScoreTracksOutcomes(t *testing.T) {
	pool, _ := newQualityTestPool(1)
	proxies := NewProxyPool(pool, []string{"http://a.example:8080#residential"})
	proxy := "http://a.example:8080#residential"
	if statuses := proxies.Status(); statuses[0].Score != 100 {
		t.Fatalf("初始评分 = %d", statuses[0].Score)
	}
	proxies.ReportSuccess(proxy)
	proxies.ReportRisk(proxy)
	if statuses := proxies.Status(); statuses[0].Score != 50 {
		t.Fatalf("一次成功一次风控后评分 = %d", statuses[0].Score)
	}
	proxies.ReportRisk(proxy)
	proxies.ReportRisk(proxy)
	if statuses := proxies.Status(); statuses[0].Score != 25 {
		t.Fatalf("三次风控后评分 = %d", statuses[0].Score)
	}
}

// TestProxyPoolPrefersHealthyProxyWithinQuality 验证同质量代理中优先选择评分更高的代理
func TestProxyPoolPrefersHealthyProxyWithinQuality(t *testing.T) {
	pool, _ := newQualityTestPool(0)
	proxies := NewProxyPool(pool, []string{
		"http://a.example:8080#residential",
		"http://b.example:8080#residential",
	})
	proxies.ReportRisk("http://a.example:8080#residential")
	proxies.ReportRisk("http://a.example:8080#residential")
	if next := proxies.Next(); next != "http://b.example:8080#residential" {
		t.Fatalf("Next() = %q，应选择评分更高的代理", next)
	}
}

// TestProxyPoolKeepsQualityAboveScore 验证质量等级仍优先于健康评分
func TestProxyPoolKeepsQualityAboveScore(t *testing.T) {
	pool, _ := newQualityTestPool(0)
	proxies := NewProxyPool(pool, []string{
		"http://dc.example:8080#datacenter",
		"http://res.example:8080#residential",
	})
	for range 5 {
		proxies.ReportRisk("http://res.example:8080#residential")
	}
	if next := proxies.Next(); next != "http://res.example:8080#residential" {
		t.Fatalf("Next() = %q，质量等级应优先于评分", next)
	}
}
