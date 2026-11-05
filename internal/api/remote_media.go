package api

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// remoteMediaUserAgent 是下载外部媒体时声明的客户端标识
const remoteMediaUserAgent = "AIStudio2API (+https://github.com/Mag1cFall/AIStudio2API)"

const (
	remoteMediaMaxBytes     int64 = 32 << 20
	remoteMediaRequestBytes int64 = 64 << 20
	remoteMediaGlobalBytes  int64 = 256 << 20
	remoteMediaMaxPixels          = 40_000_000
	remoteMediaMaxDimension       = 10_000
)

var remoteMediaGlobalBudget = struct {
	sync.Mutex
	used    int64
	changed chan struct{}
}{changed: make(chan struct{})}

var remoteImageDecodeMu sync.Mutex

type remoteMediaReservation struct {
	mu       sync.Mutex
	global   int64
	request  *remoteMediaRequestBudget
	released bool
}

type remoteMediaRequestBudget struct {
	mu           sync.Mutex
	used         int64
	reservations []*remoteMediaReservation
	released     bool
}

func (b *remoteMediaRequestBudget) reserve(ctx context.Context, n int64) (*remoteMediaReservation, error) {
	b.mu.Lock()
	if b.released || n <= 0 || n > remoteMediaMaxBytes || b.used+n > remoteMediaRequestBytes {
		b.mu.Unlock()
		return nil, fmt.Errorf("media request exceeds %d byte budget", remoteMediaRequestBytes)
	}
	b.used += n
	b.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			b.mu.Lock()
			b.used -= n
			b.mu.Unlock()
			return nil, err
		}
		remoteMediaGlobalBudget.Lock()
		if remoteMediaGlobalBudget.used+n <= remoteMediaGlobalBytes {
			remoteMediaGlobalBudget.used += n
			remoteMediaGlobalBudget.Unlock()
			reservation := &remoteMediaReservation{global: n, request: b}
			b.mu.Lock()
			if b.released {
				b.used -= n
				b.mu.Unlock()
				reservation.releaseGlobal(n)
				return nil, fmt.Errorf("media request budget was released")
			}
			b.reservations = append(b.reservations, reservation)
			b.mu.Unlock()
			return reservation, nil
		}
		changed := remoteMediaGlobalBudget.changed
		remoteMediaGlobalBudget.Unlock()
		select {
		case <-ctx.Done():
			b.mu.Lock()
			b.used -= n
			b.mu.Unlock()
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (b *remoteMediaRequestBudget) release() {
	b.mu.Lock()
	if b.released {
		b.mu.Unlock()
		return
	}
	b.released = true
	reservations := append([]*remoteMediaReservation(nil), b.reservations...)
	b.mu.Unlock()
	for _, reservation := range reservations {
		reservation.release()
	}
}

func (r *remoteMediaReservation) resize(n int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released || n < 0 || n > r.global {
		return fmt.Errorf("invalid media reservation size %d", n)
	}
	delta := r.global - n
	if delta == 0 {
		return nil
	}
	r.request.mu.Lock()
	r.request.used -= delta
	r.request.mu.Unlock()
	r.global = n
	r.releaseGlobal(delta)
	return nil
}

func (r *remoteMediaReservation) release() {
	r.mu.Lock()
	if r.released {
		r.mu.Unlock()
		return
	}
	r.released = true
	n := r.global
	r.global = 0
	r.mu.Unlock()
	r.request.mu.Lock()
	r.request.used -= n
	r.request.mu.Unlock()
	r.releaseGlobal(n)
}

func (r *remoteMediaReservation) releaseGlobal(n int64) {
	if n <= 0 {
		return
	}
	remoteMediaGlobalBudget.Lock()
	remoteMediaGlobalBudget.used -= n
	close(remoteMediaGlobalBudget.changed)
	remoteMediaGlobalBudget.changed = make(chan struct{})
	remoteMediaGlobalBudget.Unlock()
}

func holdRemoteMediaReservation(blob *aistudio.Blob, reservation *remoteMediaReservation) {
	runtime.SetFinalizer(blob, func(*aistudio.Blob) { reservation.release() })
}

func validateImagePayload(data []byte) error {
	if int64(len(data)) > remoteMediaMaxBytes {
		return fmt.Errorf("image exceeds %d bytes", remoteMediaMaxBytes)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > remoteMediaMaxDimension || config.Height > remoteMediaMaxDimension || int64(config.Width)*int64(config.Height) > remoteMediaMaxPixels {
		return fmt.Errorf("image dimensions exceed %dx%d or %d pixels", remoteMediaMaxDimension, remoteMediaMaxDimension, remoteMediaMaxPixels)
	}
	return nil
}

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

// inlineRemoteMediaWithBudget downloads media and returns an explicit release
// for the request lifetime. Keep it until all consumers finish with the blobs.
func inlineRemoteMediaWithBudget(ctx context.Context, contents []aistudio.Content) (func(), error) {
	downloaded := make(map[string]*aistudio.Blob)
	budget := &remoteMediaRequestBudget{}
	for contentIndex := range contents {
		for partIndex := range contents[contentIndex].Parts {
			part := &contents[contentIndex].Parts[partIndex]
			if part.File == nil || !remoteMediaURL(part.File.ID) || geminiFilesAPIURI(part.File.ID) {
				continue
			}
			blob, ok := downloaded[part.File.ID]
			if !ok {
				var err error
				if blob, err = downloadRemoteMediaWithBudget(ctx, budget, part.File.ID, part.File.MIME); err != nil {
					budget.release()
					return nil, fmt.Errorf("Error while downloading %s: %w", part.File.ID, err)
				}
				downloaded[part.File.ID] = blob
			}
			part.DisplayName = cmp.Or(part.DisplayName, part.File.Name)
			part.File, part.InlineData = nil, blob
		}
	}
	return budget.release, nil
}

// inlineRemoteMedia is the compatibility wrapper for existing handlers.
func inlineRemoteMedia(ctx context.Context, contents []aistudio.Content) error {
	_, err := inlineRemoteMediaWithBudget(ctx, contents)
	return err
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
	return downloadRemoteMediaWithBudget(ctx, &remoteMediaRequestBudget{}, url, declared)
}

func downloadRemoteMediaWithBudget(ctx context.Context, budget *remoteMediaRequestBudget, url string, declared string) (*aistudio.Blob, error) {
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
	if response.ContentLength > remoteMediaMaxBytes {
		return nil, fmt.Errorf("content exceeds %d bytes", remoteMediaMaxBytes)
	}
	reservation, err := budget.reserve(ctx, remoteMediaMaxBytes)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, remoteMediaMaxBytes+1))
	if err != nil {
		reservation.release()
		return nil, err
	}
	if int64(len(data)) > remoteMediaMaxBytes {
		reservation.release()
		return nil, fmt.Errorf("content exceeds %d bytes", remoteMediaMaxBytes)
	}
	if len(data) == 0 {
		reservation.release()
		return nil, fmt.Errorf("response body is empty")
	}
	mimeType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mimeType == "application/octet-stream" || mimeType == "binary/octet-stream" {
		mimeType = declared
		if mimeType == "" {
			mimeType = detectMediaType("", data)
		}
	}
	if strings.HasPrefix(strings.ToLower(mimeType), "image/") || (len(data) >= 3 && string(data[:3]) == "GIF") {
		if err := validateImagePayload(data); err != nil {
			reservation.release()
			return nil, err
		}
	}
	mimeType, data = normalizeImagePayload(mimeType, data)
	if int64(len(data)) > remoteMediaMaxBytes {
		reservation.release()
		return nil, fmt.Errorf("normalized content exceeds %d bytes", remoteMediaMaxBytes)
	}
	if err := reservation.resize(int64(len(data))); err != nil {
		reservation.release()
		return nil, err
	}
	blob := &aistudio.Blob{MIME: mimeType, Data: data}
	holdRemoteMediaReservation(blob, reservation)
	return blob, nil
}
