package aistudio

import (
	"fmt"
	"net/http"
	"sync/atomic"
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

// TestEvictClientSkipsInflight 验证有在途请求的客户端不被淘汰：HTTP/3 连接关闭会中止活跃流
func TestEvictClientSkipsInflight(t *testing.T) {
	now := time.Now()
	busy := &atomic.Int64{}
	busy.Store(1)
	transport := &MakerSuiteHTTPTransport{
		now:            func() time.Time { return now },
		clients:        make(map[string]*http.Client, 2),
		clientUsed:     make(map[string]time.Time, 2),
		clientInflight: make(map[string]*atomic.Int64, 2),
	}
	transport.clients["socks5://127.0.0.1:10000"] = &http.Client{}
	transport.clientUsed["socks5://127.0.0.1:10000"] = now
	transport.clientInflight["socks5://127.0.0.1:10000"] = busy
	transport.clients["socks5://127.0.0.1:10001"] = &http.Client{}
	transport.clientUsed["socks5://127.0.0.1:10001"] = now.Add(time.Second)
	transport.clientInflight["socks5://127.0.0.1:10001"] = &atomic.Int64{}
	transport.evictClientLocked()
	if _, exists := transport.clients["socks5://127.0.0.1:10000"]; !exists {
		t.Fatal("有在途请求的最老客户端不应被淘汰")
	}
	if _, exists := transport.clients["socks5://127.0.0.1:10001"]; exists {
		t.Fatal("应淘汰空闲客户端而不是在途客户端")
	}
}
