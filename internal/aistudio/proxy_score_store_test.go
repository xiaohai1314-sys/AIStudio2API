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
