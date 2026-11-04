package api

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

// TestClientAddressTrustsForwardedForOnlyFromProxy 验证只有可信代理的 X-Forwarded-For 参与客户端身份
func TestClientAddressTrustsForwardedForOnlyFromProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("10.0.0.0/8")}
	cases := []struct {
		name       string
		remoteAddr string
		forwarded  string
		want       string
	}{
		{name: "非可信对端忽略转发头", remoteAddr: "198.51.100.9:1234", forwarded: "203.0.113.7", want: "198.51.100.9"},
		{name: "可信对端取转发地址", remoteAddr: "127.0.0.1:1234", forwarded: "203.0.113.7", want: "203.0.113.7"},
		{name: "从右向左跳过可信代理", remoteAddr: "127.0.0.1:1234", forwarded: "203.0.113.7, 10.0.0.5", want: "203.0.113.7"},
		{name: "转发地址带端口", remoteAddr: "127.0.0.1:1234", forwarded: "203.0.113.7:5555", want: "203.0.113.7"},
		{name: "无转发头回退对端", remoteAddr: "127.0.0.1:1234", want: "127.0.0.1"},
		{name: "非法转发头回退对端", remoteAddr: "127.0.0.1:1234", forwarded: "not-an-address", want: "127.0.0.1"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://example.com/", nil)
			request.RemoteAddr = test.remoteAddr
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			if got := clientAddress(request, trusted); got != test.want {
				t.Fatalf("clientAddress() = %q, want %q", got, test.want)
			}
		})
	}
}
