package aistudio

import (
	"context"
	"testing"
)

// TestAccountPoolLimitsEgressConcurrency 验证同一固定代理的并发请求数受出口上限约束
func TestAccountPoolLimitsEgressConcurrency(t *testing.T) {
	proxy := "http://shared.example:8080#residential"
	accounts := []*Account{
		{ID: "a@example.com", Config: AccountConfig{Enabled: true, Proxy: proxy}, State: AccountReady},
		{ID: "b@example.com", Config: AccountConfig{Enabled: true, Proxy: proxy}, State: AccountReady},
	}
	pool := NewAccountPool(accounts, 1)
	pool.SetProxyLimits(1, "")
	first, _, err := pool.TryAcquireFor(context.Background(), AccountSelection{})
	if err != nil || first == nil {
		t.Fatalf("首次获取 = %v, %v", first, err)
	}
	if lease, waitable, err := pool.TryAcquireFor(context.Background(), AccountSelection{}); err != nil || lease != nil || !waitable {
		t.Fatalf("超过出口并发上限仍取得租约: lease=%v waitable=%v err=%v", lease, waitable, err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, _, err := pool.TryAcquireFor(context.Background(), AccountSelection{})
	if err != nil || second == nil {
		t.Fatalf("释放后获取 = %v, %v", second, err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

// TestAccountPoolLimitsGlobalProxyConcurrency 验证未绑定代理池的账户按全局代理统计出口并发
func TestAccountPoolLimitsGlobalProxyConcurrency(t *testing.T) {
	pool, _ := newQualityTestPool(2)
	pool.SetPerAccountConcurrency(1)
	pool.SetProxyLimits(1, "http://global.example:8080")
	first, _, err := pool.TryAcquireFor(context.Background(), AccountSelection{})
	if err != nil || first == nil {
		t.Fatalf("首次获取 = %v, %v", first, err)
	}
	if lease, waitable, err := pool.TryAcquireFor(context.Background(), AccountSelection{}); err != nil || lease != nil || !waitable {
		t.Fatalf("全局代理超过并发上限仍取得租约: lease=%v waitable=%v err=%v", lease, waitable, err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, _, err := pool.TryAcquireFor(context.Background(), AccountSelection{})
	if err != nil || second == nil {
		t.Fatalf("释放后获取 = %v, %v", second, err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}
