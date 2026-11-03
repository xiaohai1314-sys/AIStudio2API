package aistudio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProxyScoresPersistAcrossPools 验证评分落盘后新实例能恢复，且文件不含代理地址与凭据
func TestProxyScoresPersistAcrossPools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-scores.json")
	proxies := []string{
		"socks5://user:secret@a.example:1080#residential",
		"http://b.example:8080#residential",
	}
	pool, _ := newQualityTestPool(0)
	first := NewProxyPool(pool, proxies)
	if err := first.LoadScores(path); err != nil {
		t.Fatal(err)
	}
	first.ReportSuccess(proxies[0])
	first.ReportRisk(proxies[0])
	first.ReportRisk(proxies[1])
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "a.example") {
		t.Fatalf("评分文件泄露代理地址: %s", data)
	}
	second := NewProxyPool(pool, proxies)
	if err := second.LoadScores(path); err != nil {
		t.Fatal(err)
	}
	statuses := second.Status()
	if statuses[0].Score != 50 || statuses[1].Score != 0 {
		t.Fatalf("恢复后的评分 = %d, %d", statuses[0].Score, statuses[1].Score)
	}
}

// TestProxyScoresIgnoreExpiredSamples 验证超过窗口的样本在加载时被忽略
func TestProxyScoresIgnoreExpiredSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-scores.json")
	proxy := "http://a.example:8080#residential"
	data, err := json.Marshal(proxyScoreFile{
		Version: 1,
		Proxies: map[string]proxyScoreSample{
			proxyScoreKey(proxy): {Good: 5, Since: time.Now().Add(-proxyScoreWindow - time.Minute)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	pool, _ := newQualityTestPool(0)
	proxies := NewProxyPool(pool, []string{proxy})
	if err := proxies.LoadScores(path); err != nil {
		t.Fatal(err)
	}
	if score := proxies.Status()[0].Score; score != 100 {
		t.Fatalf("过期样本影响了评分: %d", score)
	}
}

// TestProxyScoresRejectCorruptFile 验证损坏的评分文件返回错误而不影响代理池
func TestProxyScoresRejectCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-scores.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	pool, _ := newQualityTestPool(0)
	proxies := NewProxyPool(pool, []string{"http://a.example:8080"})
	if err := proxies.LoadScores(path); err == nil {
		t.Fatal("损坏的评分文件没有报错")
	}
	if score := proxies.Status()[0].Score; score != 100 {
		t.Fatalf("损坏文件影响了评分: %d", score)
	}
}

// TestProxyForbiddenHistoryPersists 验证 403 计数随评分文件一起保存与恢复
func TestProxyForbiddenHistoryPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-scores.json")
	proxy := "http://a.example:8080#residential"
	pool, _ := newQualityTestPool(0)
	first := NewProxyPool(pool, []string{proxy})
	if err := first.LoadScores(path); err != nil {
		t.Fatal(err)
	}
	first.ReportForbidden(proxy)
	first.ReportSuccess(proxy)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := NewProxyPool(pool, []string{proxy})
	if err := second.LoadScores(path); err != nil {
		t.Fatal(err)
	}
	if status := second.Status()[0]; status.ForbiddenRate != 50 || status.Score != 50 {
		t.Fatalf("恢复后的 403 率与评分 = %d, %d", status.ForbiddenRate, status.Score)
	}
}

// TestProxyHistoryScoresAfterShortWindowExpires 验证短窗口过期后仍使用 24 小时历史评分
func TestProxyHistoryScoresAfterShortWindowExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-scores.json")
	proxy := "http://a.example:8080#residential"
	hour := time.Now().Add(-3 * time.Hour).Truncate(time.Hour)
	data, err := json.Marshal(proxyScoreFile{
		Version: 2,
		Proxies: map[string]proxyScoreSample{
			proxyScoreKey(proxy): {
				Good: 1, Bad: 3, Since: time.Now().Add(-time.Hour),
				History: []proxyScoreBucket{{Hour: hour, Good: 1, Bad: 3, Forbidden: 3}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	pool, _ := newQualityTestPool(0)
	proxies := NewProxyPool(pool, []string{proxy})
	if err := proxies.LoadScores(path); err != nil {
		t.Fatal(err)
	}
	status := proxies.Status()[0]
	if status.Score != 25 || status.ForbiddenRate != 75 {
		t.Fatalf("长期历史未生效: 评分=%d 403率=%d", status.Score, status.ForbiddenRate)
	}
}

// TestProxyHistoryDropsExpiredBuckets 验证超过 24 小时的分桶在汇总时被忽略
func TestProxyHistoryDropsExpiredBuckets(t *testing.T) {
	pool, _ := newQualityTestPool(0)
	proxies := NewProxyPool(pool, []string{"http://a.example:8080"})
	entry := proxies.entries[0]
	now := time.Now().UTC()
	old := now.Add(-proxyHistoryWindow - time.Hour).Truncate(time.Hour)
	entry.history[proxyHistoryIndex(old)] = proxyScoreBucket{Hour: old, Bad: 10, Forbidden: 10}
	if score := entry.scoreLocked(now); score != 100 {
		t.Fatalf("过期分桶影响了评分: %d", score)
	}
	if rate := entry.forbiddenRateLocked(now); rate != 0 {
		t.Fatalf("过期分桶影响了 403 率: %d", rate)
	}
}
