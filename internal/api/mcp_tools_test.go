package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// TestConnectMCPRejectsPrivateTargets 验证解析到本机、私网或链路本地的 MCP 服务器被拒绝
func TestConnectMCPRejectsPrivateTargets(t *testing.T) {
	server := &server{}
	for _, target := range []string{
		"http://127.0.0.1:9/mcp",
		"http://10.0.0.1:9/mcp",
		"http://169.254.169.254/latest/meta-data",
	} {
		_, err := server.connectMCP(context.Background(), aistudio.MCPServer{Name: "local", URL: target}, "tools/list")
		if err == nil || !strings.Contains(err.Error(), "non-public") {
			t.Fatalf("%s: err = %v", target, err)
		}
	}
}

func TestConnectMCPRejectsEgressProxyBeforeContactingProxy(t *testing.T) {
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyRequests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()

	server := &server{config: Config{EgressProxy: func() string { return proxy.URL }}}
	_, err := server.connectMCP(context.Background(), aistudio.MCPServer{
		Name: "proxied", URL: "http://127.0.0.1:9/mcp",
	}, "tools/list")
	if err == nil || !strings.Contains(err.Error(), "EgressProxy") {
		t.Fatalf("connect error = %v, want explicit proxy security rejection", err)
	}
	if got := proxyRequests.Load(); got != 0 {
		t.Fatalf("proxy received %d requests despite fail-closed policy", got)
	}
}

func TestMCPRedirectRejectsPrivateTargetBeforeRequest(t *testing.T) {
	var roundTrips atomic.Int32
	client := &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			roundTrips.Add(1)
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"http://127.0.0.1:9/mcp"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		}),
		CheckRedirect: checkMCPRedirect,
	}
	_, err := client.Get("https://public.example/mcp")
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("redirect error = %v, want private target rejection", err)
	}
	if got := roundTrips.Load(); got != 1 {
		t.Fatalf("RoundTrip calls = %d, private redirect target must not be requested", got)
	}
}

func TestMCPRedirectDNSRebindingBlockedAtDial(t *testing.T) {
	var lookups atomic.Int32
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		if lookups.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	request := httptest.NewRequest(http.MethodGet, "https://mcp.example/mcp", nil)
	if err := checkMCPRedirectWithHostCheck(request, nil, func(ctx context.Context, host string) error {
		return checkRemoteMediaHostWithLookup(ctx, host, lookup)
	}); err != nil {
		t.Fatalf("redirect validation rejected initial public answer: %v", err)
	}
	_, err := dialPublicRemoteWithLookup(context.Background(), "tcp", "mcp.example:443", lookup)
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("dial error = %v, want rebound private address rejection", err)
	}
	if got := lookups.Load(); got != 2 {
		t.Fatalf("DNS lookups = %d, want validation and fresh dial lookup", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
