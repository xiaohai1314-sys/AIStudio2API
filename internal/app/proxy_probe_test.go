package app

import (
	"net/http"
	"testing"
)

// TestProxyProbeStatus 验证代理认证失败与上游不可用会被探测判定为不可用
func TestProxyProbeStatus(t *testing.T) {
	for _, status := range []int{
		http.StatusProxyAuthRequired, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable,
	} {
		if proxyProbeStatus(status) == nil {
			t.Fatalf("HTTP %d 应判定为不可用", status)
		}
	}
	for _, status := range []int{
		http.StatusOK, http.StatusFound, http.StatusForbidden, http.StatusNotFound,
	} {
		if err := proxyProbeStatus(status); err != nil {
			t.Fatalf("HTTP %d 应判定为可用: %v", status, err)
		}
	}
}
