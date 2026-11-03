package aistudio

import (
	"testing"
	"time"
)

// TestRecordEndpointCountsOutcomes 验证按模型与通道累计成功与风控计数
func TestRecordEndpointCountsOutcomes(t *testing.T) {
	pool := NewAccountPool(nil, 1)
	pool.RecordEndpoint("gemini-pro", ChannelPlayground, EndpointSuccess)
	pool.RecordEndpoint("gemini-pro", ChannelPlayground, EndpointChallenge)
	pool.RecordEndpoint("gemini-pro", ChannelPlayground, EndpointForbidden)
	pool.RecordEndpoint("gemini-pro", ChannelPlayground, EndpointRateLimited)
	pool.RecordEndpoint("gemini-pro", ChannelPlayground, EndpointFailed)
	risks := pool.EndpointRisks()
	if len(risks) != 1 {
		t.Fatalf("端点数 = %d", len(risks))
	}
	risk := risks[0]
	if risk.Requests != 5 || risk.Succeeded != 1 || risk.Failed != 4 {
		t.Fatalf("计数 = %+v", risk)
	}
	if risk.Challenge != 1 || risk.Forbidden != 1 || risk.RateLimited != 1 {
		t.Fatalf("风控计数 = %+v", risk)
	}
	if risk.LastAt.IsZero() {
		t.Fatal("最近时间没有记录")
	}
}

// TestEndpointRisksSortsByHits 验证风控命中多的端点排在前面
func TestEndpointRisksSortsByHits(t *testing.T) {
	pool := NewAccountPool(nil, 1)
	pool.RecordEndpoint("quiet", ChannelPlayground, EndpointSuccess)
	pool.RecordEndpoint("noisy", ChannelBuild, EndpointForbidden)
	pool.RecordEndpoint("noisy", ChannelBuild, EndpointChallenge)
	risks := pool.EndpointRisks()
	if len(risks) != 2 || risks[0].Model != "noisy" {
		t.Fatalf("排序 = %+v", risks)
	}
}

// TestEndpointRisksPrunesExpiredWindow 验证超过窗口的端点计数被清理
func TestEndpointRisksPrunesExpiredWindow(t *testing.T) {
	pool := NewAccountPool(nil, 1)
	pool.RecordEndpoint("gemini-pro", ChannelPlayground, EndpointSuccess)
	pool.mu.Lock()
	pool.endpointRisk[string(ChannelPlayground)+":gemini-pro"].since = time.Now().Add(-endpointRiskWindow - time.Minute)
	pool.mu.Unlock()
	if risks := pool.EndpointRisks(); len(risks) != 0 {
		t.Fatalf("过期端点仍然返回: %+v", risks)
	}
}

// TestRecordEndpointSkipsEmptyModel 验证没有模型时不记录
func TestRecordEndpointSkipsEmptyModel(t *testing.T) {
	pool := NewAccountPool(nil, 1)
	pool.RecordEndpoint(" ", ChannelPlayground, EndpointSuccess)
	if risks := pool.EndpointRisks(); len(risks) != 0 {
		t.Fatalf("空模型被记录: %+v", risks)
	}
}
