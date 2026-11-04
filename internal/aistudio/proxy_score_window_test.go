package aistudio

import (
	"testing"
	"time"
)

// TestProxyScoreIgnoresExpiredShortWindow 验证短窗口过期后不再用旧样本压低评分
func TestProxyScoreIgnoresExpiredShortWindow(t *testing.T) {
	pool, _ := newQualityTestPool(0)
	proxies := NewProxyPool(pool, []string{"http://a.example:8080#residential"})
	proxies.mu.Lock()
	entry := proxies.entries[0]
	entry.good, entry.bad = 0, 5
	entry.scoreSince = time.Now().Add(-proxyScoreWindow - time.Minute)
	proxies.mu.Unlock()
	if score := proxies.Status()[0].Score; score != 100 {
		t.Fatalf("过期短窗口样本影响了评分: %d", score)
	}
}
