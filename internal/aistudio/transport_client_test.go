package aistudio

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestEvictClientRemovesOldest 验证固定出口客户端超过上限时关闭并移除最久未使用的客户端
func TestEvictClientRemovesOldest(t *testing.T) {
	now := time.Now()
	transport := &MakerSuiteHTTPTransport{
		now:        func() time.Time { return now },
		clients:    make(map[string]*http.Client, maxProxyClients),
		clientUsed: make(map[string]time.Time, maxProxyClients),
	}
	for index := range maxProxyClients {
		proxy := fmt.Sprintf("socks5://127.0.0.1:%d", 10000+index)
		transport.clients[proxy] = &http.Client{}
		transport.clientUsed[proxy] = now.Add(time.Duration(index) * time.Second)
	}
	transport.evictClientLocked()
	if len(transport.clients) != maxProxyClients-1 {
		t.Fatalf("淘汰后客户端数量 = %d, want %d", len(transport.clients), maxProxyClients-1)
	}
	if len(transport.clientUsed) != maxProxyClients-1 {
		t.Fatalf("淘汰后使用时间记录数量 = %d, want %d", len(transport.clientUsed), maxProxyClients-1)
	}
	if _, exists := transport.clients["socks5://127.0.0.1:10000"]; exists {
		t.Fatal("最久未使用的客户端应当被移除")
	}
}
