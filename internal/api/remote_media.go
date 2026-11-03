package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// remoteMediaUserAgent 是下载外部媒体时声明的客户端标识
const remoteMediaUserAgent = "AIStudio2API (+https://github.com/Mag1cFall/AIStudio2API)"

// publicOutboundTransport 禁用环境代理并在实际拨号前重新解析及验证地址，避免校验后 DNS 重绑定到内网。
var publicOutboundTransport = newPublicOutboundTransport()

// remoteMediaClient 是下载外部媒体的 HTTP 客户端，每次重定向前检查目标主机。
var remoteMediaClient = &http.Client{Transport: publicOutboundTransport, CheckRedirect: func(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return checkRemoteMediaHost(request.Context(), request.URL.Hostname())
}}

func newPublicOutboundTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialPublicRemote
	return transport
}

// inlineRemoteMedia 把内容中 http(s) URL 形式的文件引用下载为内联媒体，同一 URL 只下载一次，Gemini Files API 的文件 URI 保持文件引用
func inlineRemoteMedia(ctx context.Context, contents []aistudio.Content) error {
	downloaded := make(map[string]*aistudio.Blob)
	for contentIndex := range contents {
		for partIndex := range contents[contentIndex].Parts {
			part := &contents[contentIndex].Parts[partIndex]
			if part.File == nil || !remoteMediaURL(part.File.ID) || geminiFilesAPIURI(part.File.ID) {
				continue
			}
			blob, ok := downloaded[part.File.ID]
			if !ok {
				var err error
				if blob, err = downloadRemoteMedia(ctx, part.File.ID, part.File.MIME); err != nil {
					return fmt.Errorf("Error while downloading %s: %w", part.File.ID, err)
				}
				downloaded[part.File.ID] = blob
			}
			part.DisplayName = cmp.Or(part.DisplayName, part.File.Name)
			part.File, part.InlineData = nil, blob
		}
	}
	return nil
}

// remoteMediaURL 返回值是否为 http 或 https URL
func remoteMediaURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// checkRemoteMediaHost 拒绝解析到本机、私网、链路本地、组播或未指定地址的主机。
// 这个检查也用于 webhook，避免公开 API 触发对服务端内网资源的请求。
func checkRemoteMediaHost(ctx context.Context, host string) error {
	_, err := publicRemoteAddresses(ctx, host)
	return err
}

// publicRemoteAddresses 解析并返回可安全建立出站连接的公网地址。
func publicRemoteAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if strings.TrimSpace(host) == "" {
		return nil, errors.New("host is required")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("host %s resolves to no addresses", host)
	}
	public := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !publicRemoteAddress(address) {
			return nil, fmt.Errorf("host %s resolves to non-public address %s", host, address)
		}
		public = append(public, address)
	}
	return public, nil
}

// publicRemoteAddress 判断地址是否适合作为公开 API 能控制的出站请求目标。
func publicRemoteAddress(address netip.Addr) bool {
	return address.IsValid() && address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLinkLocalUnicast() && !address.IsMulticast() &&
		!address.IsUnspecified() && !reservedRemoteAddress(address)
}

// reservedRemotePrefixes 补充 netip.IsPrivate 未覆盖、但不应作为 API 控制的服务端请求目标的 IPv4 地址段。
var reservedRemotePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // current network
	netip.MustParsePrefix("100.64.0.0/10"),   // shared carrier-grade NAT
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmark testing
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved and broadcast
	netip.MustParsePrefix("2001:db8::/32"),   // IPv6 documentation
}

func reservedRemoteAddress(address netip.Addr) bool {
	for _, prefix := range reservedRemotePrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// validatePublicHTTPURL 只接受没有用户凭据且解析到公网地址的 HTTP(S) URL。
func validatePublicHTTPURL(ctx context.Context, raw string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("must be a public http or https URL: %q", raw)
	}
	if err := checkRemoteMediaHost(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

// dialPublicRemote 在实际连接时重新解析和验证主机，并直接拨号到该 IP，阻止 DNS 在校验后切换到内网地址。
func dialPublicRemote(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return nil, fmt.Errorf("invalid remote address %q", address)
	}
	addresses, err := publicRemoteAddresses(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for _, resolved := range addresses {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// downloadRemoteMedia 在请求 context 内下载外部媒体，MIME 依次取响应 Content-Type、声明值与内容判断
func downloadRemoteMedia(ctx context.Context, url string, declared string) (*aistudio.Blob, error) {
	parsed, err := validatePublicHTTPURL(ctx, url)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", remoteMediaUserAgent)
	response, err := remoteMediaClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	if response.ContentLength > openAIFileMaxBytes {
		return nil, fmt.Errorf("content exceeds %d bytes", openAIFileMaxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, openAIFileMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > openAIFileMaxBytes {
		return nil, fmt.Errorf("content exceeds %d bytes", openAIFileMaxBytes)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}
	mimeType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mimeType == "application/octet-stream" || mimeType == "binary/octet-stream" {
		mimeType = declared
		if mimeType == "" {
			mimeType = detectMediaType("", data)
		}
	}
	mimeType, data = normalizeImagePayload(mimeType, data)
	return &aistudio.Blob{MIME: mimeType, Data: data}, nil
}
