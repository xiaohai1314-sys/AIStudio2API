package aistudio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/camoufoxnative"
)

const (
	// MakerSuiteRPCBase 是 AI Studio RPC 根地址
	MakerSuiteRPCBase = "https://alkalimakersuite-pa.clients6.google.com/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/"
	// JSONProtobufContentType 是 MakerSuite 使用的数组协议媒体类型
	JSONProtobufContentType           = "application/json+protobuf"
	maxHTMLResponseBytes        int64 = 16 << 20
	maxPageResourceBytes        int64 = 32 << 20
	maxWAAResponseBytes         int64 = 32 << 20
	maxUploadResponseBytes      int64 = 1 << 20
	maxDriveResponseBytes       int64 = 1 << 20
	maxInteractionResponseBytes int64 = 64 << 20
)

// RPCRequest 描述交给认证传输层的单次 MakerSuite 请求
type RPCRequest struct {
	Method    string
	URL       string
	AccountID string
	RequestID string
	Header    http.Header
	Body      []byte
	Streaming bool
}

// RPCResponse 描述认证传输层返回的响应头和实时正文，Proof 为受保护请求写入的 WAA proof
type RPCResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
	Proof      string
}

// RPCTransport 负责认证、账户租约、Cookie 写回和真实网络发送
type RPCTransport interface {
	Do(context.Context, RPCRequest) (*RPCResponse, error)
}

// ProtectedTransport 原子完成 fresh WAA proof、field 5 写入和同 context 发送
type ProtectedTransport interface {
	DoProtected(context.Context, GenerateRequest, RPCRequest) (*RPCResponse, error)
}

// RequestContext 保存账户运行时提供的协议上下文
type RequestContext struct {
	Timezone string
}

// RequestContextProvider 按账户返回当前协议上下文
type RequestContextProvider interface {
	RequestContext(context.Context, string) (RequestContext, error)
}

// ClientOptions 定义协议客户端的窄依赖
type ClientOptions struct {
	Transport       RPCTransport
	Protected       ProtectedTransport
	ContextProvider RequestContextProvider
	// AccountRateLimit 是单账户每分钟最多发出的上游请求数，0 表示不限制
	AccountRateLimit int
	// AccountRateJitter 是请求间隔的随机抖动百分比
	AccountRateJitter int
	Pages             PageCatalogProvider
	Static            StaticFetcher
}

// Client 实现 AI Studio 私有协议核心
type Client struct {
	transport       RPCTransport
	protected       ProtectedTransport
	contextProvider RequestContextProvider
	pacer           *pacer
	pages           PageCatalogProvider
	static          StaticFetcher
	catalogMu       sync.RWMutex
	catalogs        map[string]modelCatalog
	tierMu          sync.RWMutex
	tiers           map[string]BenefitTier
	termsMu         sync.Mutex
	termsAccepted   map[string]struct{}
	disclaimers     map[string]struct{}
	turns           *modelTurnHistory
	frontendMu      sync.Mutex
	// frontend 是按地址解析过的官网前端主程序推荐模型
	frontend map[string]frontendModels
}

// NewClient 创建协议客户端
func NewClient(options ClientOptions) *Client {
	return &Client{
		transport:       options.Transport,
		protected:       options.Protected,
		contextProvider: options.ContextProvider,
		pacer:           newPacer(options.AccountRateLimit, options.AccountRateJitter),
		pages:           options.Pages,
		static:          options.Static,
		catalogs:        make(map[string]modelCatalog),
		tiers:           make(map[string]BenefitTier),
		termsAccepted:   make(map[string]struct{}),
		disclaimers:     make(map[string]struct{}),
		turns:           newModelTurnHistory(),
		frontend:        make(map[string]frontendModels),
	}
}

// RPCError 保存上游状态和协议错误码
type RPCError struct {
	Method     string
	StatusCode int
	Code       int64
	Message    string
	Metadata   map[string]string
	RetryDelay time.Duration
}

// ContextOverflow 返回错误是否为上游报告的输入 token 超过模型上限
func (e *RPCError) ContextOverflow() bool {
	return e.StatusCode == http.StatusBadRequest && strings.Contains(e.Message, "input token count exceeds the maximum number of tokens allowed")
}

// accessDenial 返回官网对该 403、协议错误码 7 的消息跳转的拒绝原因，其他错误返回 nil
func (e *RPCError) accessDenial() *camoufoxnative.AccessDenial {
	if e.StatusCode != http.StatusForbidden || e.Code != 7 {
		return nil
	}
	return camoufoxnative.DenialForMessage(e.Message)
}

// Error 返回结构化上游错误
func (e *RPCError) Error() string {
	if denial := e.accessDenial(); denial != nil {
		return fmt.Sprintf("AI Studio %s 返回 HTTP %d、协议错误码 %d: %s（%v）", e.Method, e.StatusCode, e.Code, e.Message, denial)
	}
	if e.Code != 0 {
		return fmt.Sprintf("AI Studio %s 返回 HTTP %d、协议错误码 %d: %s", e.Method, e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("AI Studio %s 返回 HTTP %d: %s", e.Method, e.StatusCode, e.Message)
}

// StatusError 以 gRPC 状态名代替 HTTP 状态码写出 401 或 403 的上游方法、协议错误码与原因
func (e *RPCError) StatusError() string {
	status := "PERMISSION_DENIED"
	if e.StatusCode == http.StatusUnauthorized {
		status = "UNAUTHENTICATED"
	}
	if e.Code != 0 {
		return fmt.Sprintf("AI Studio %s 返回 %s、协议错误码 %d: %s", e.Method, status, e.Code, e.Message)
	}
	return fmt.Sprintf("AI Studio %s 返回 %s: %s", e.Method, status, e.Message)
}

// Unwrap 把官网会跳转拒绝页的错误映射为对应的 camoufoxnative.AccessDenial
func (e *RPCError) Unwrap() error {
	if denial := e.accessDenial(); denial != nil {
		return denial
	}
	return nil
}

// HTTPStatus 返回上游 HTTP 状态
func (e *RPCError) HTTPStatus() int {
	return e.StatusCode
}

// RetryAt 返回上游给出的可重试时刻，没有重试时长时返回零值
func (e *RPCError) RetryAt(now time.Time) time.Time {
	if e.RetryDelay <= 0 {
		return time.Time{}
	}
	return now.Add(e.RetryDelay)
}

// defaultUnaryRPCTimeout 是非流式 RPC 兜底超时的默认值：上游客户端无整体 Timeout（流式不能加，会杀死正常 SSE），
// 非流式调用统一封顶，避免上游或代理 hang 住时 goroutine 无限阻塞并占用账户租约
const defaultUnaryRPCTimeout = 120 * time.Second

// unaryRPCTimeoutNs 保存当前生效的非流式 RPC 兜底超时（纳秒），由 UNARY_RPC_TIMEOUT 在启动时配置
var unaryRPCTimeoutNs atomic.Int64

func init() { unaryRPCTimeoutNs.Store(int64(defaultUnaryRPCTimeout)) }

// SetUnaryRPCTimeout 设置非流式 RPC 的兜底超时，非正值忽略
func SetUnaryRPCTimeout(timeout time.Duration) {
	if timeout > 0 {
		unaryRPCTimeoutNs.Store(int64(timeout))
	}
}

// unaryRPCTimeoutValue 返回当前生效的非流式 RPC 兜底超时
func unaryRPCTimeoutValue() time.Duration {
	return time.Duration(unaryRPCTimeoutNs.Load())
}

// defaultUnaryTransferTimeout 是文件上传与下载这类流式传输的兜底超时默认值，与 REQUEST_TIMEOUT 默认一致
const defaultUnaryTransferTimeout = 30 * time.Minute

// unaryTransferTimeoutNs 保存当前生效的上传下载兜底超时（纳秒），由 REQUEST_TIMEOUT 在启动时配置
var unaryTransferTimeoutNs atomic.Int64

func init() { unaryTransferTimeoutNs.Store(int64(defaultUnaryTransferTimeout)) }

// SetUnaryTransferTimeout 设置文件上传与下载的兜底超时，非正值忽略
func SetUnaryTransferTimeout(timeout time.Duration) {
	if timeout > 0 {
		unaryTransferTimeoutNs.Store(int64(timeout))
	}
}

// unaryTransferTimeoutValue 返回当前生效的上传下载兜底超时
func unaryTransferTimeoutValue() time.Duration {
	return time.Duration(unaryTransferTimeoutNs.Load())
}

// maxUnaryResponseBytes 是非流式响应正文的读取上限，防止恶意或故障代理返回超大正文打爆内存
const maxUnaryResponseBytes int64 = 64 << 20

// maxErrorResponseBytes 限制错误响应正文，避免流式请求的错误路径无界分配内存
const maxErrorResponseBytes int64 = 1 << 20

func readErrorResponseBody(body io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxErrorResponseBytes {
		return nil, fmt.Errorf("错误响应正文超过 %d MiB", maxErrorResponseBytes>>20)
	}
	return raw, nil
}

// ReadResponseBody 读取非流式网络响应并拒绝超过 limit 的正文；不会静默截断后解析。
func ReadResponseBody(body io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("响应正文上限必须为正数")
	}
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("响应正文超过 %d MiB", limit>>20)
	}
	return raw, nil
}

// DrainResponseBody 消费并校验非流式响应正文上限，不保留正文。
func DrainResponseBody(body io.Reader, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("响应正文上限必须为正数")
	}
	var count limitedCountWriter
	if _, err := io.Copy(&count, io.LimitReader(body, limit+1)); err != nil {
		return count.n, err
	}
	if count.n > limit {
		return count.n, fmt.Errorf("响应正文超过 %d MiB", limit>>20)
	}
	return count.n, nil
}

type limitedCountWriter struct{ n int64 }

func (w *limitedCountWriter) Write(p []byte) (int, error) { w.n += int64(len(p)); return len(p), nil }

func (c *Client) do(ctx context.Context, method string, accountID string, requestID string, body []byte, streaming bool) (*RPCResponse, error) {
	return c.send(ctx, newRPCRequest(method, accountID, requestID, body, streaming))
}

// unaryRetryAttempts 是非流式 RPC 在瞬时传输错误上的最多尝试次数（含首次）
const unaryRetryAttempts = 3

// unaryRetryBackoff 是非流式 RPC 瞬时传输错误重试前的等待时长，按尝试次数递增
var unaryRetryBackoff = []time.Duration{200 * time.Millisecond, 500 * time.Millisecond}

// isTransientTransportError 判断传输错误是否为可重试的瞬时错误：
// 服务端/代理关闭了复用的空闲连接（EOF）、连接被重置或断开时，重发一次通常就能成功。
// 调用方取消或超时不重试，避免掩盖真实的取消与缓慢。
func isTransientTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}

// send 发送 RPC 请求并校验响应状态与媒体类型。fork 的三项行为保留在这个统一入口：
// 先过 pacer、非流式加超时 ctx、非流式响应同步有界读完再返回
func (c *Client) send(ctx context.Context, rpc RPCRequest) (*RPCResponse, error) {
	if err := c.pacer.Wait(ctx, rpc.AccountID); err != nil {
		return nil, err
	}
	if !rpc.Streaming {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, unaryRPCTimeoutValue())
		defer cancel()
	}
	c.applyBenefitTier(rpc.Method, rpc.AccountID, rpc.Header)
	// 非流式 RPC 遇到瞬时传输错误时重试：请求体是内存中的 []byte，可安全重发；
	// 常见原因是复用的空闲连接被服务端/代理关闭（EOF），重发一次通常就能成功。
	// 流式请求不重试，避免重复发送已部分发出的流。
	attempts := 1
	if !rpc.Streaming {
		attempts = unaryRetryAttempts
	}
	var response *RPCResponse
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(unaryRetryBackoff[attempt-1])
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		response, err = c.transport.Do(ctx, rpc)
		if err == nil {
			break
		}
		if !isTransientTransportError(err) {
			break
		}
		slog.Debug("非流式 RPC 瞬时传输错误，重试", "method", rpc.Method, "attempt", attempt+1, "error", err)
	}
	if err != nil {
		return nil, fmt.Errorf("发送 AI Studio %s: %w", rpc.Method, err)
	}
	if !rpc.Streaming {
		// 非流式响应在此同步读完再返回：超时 ctx 取消后 Body 读取也会失败，
		// leaseResponseBody 的租约释放与调用方返回不会因上游 hang 而无限推迟
		defer response.Body.Close()
		maxBytes := maxUnaryResponseBytes
		if response.StatusCode != http.StatusOK {
			maxBytes = maxErrorResponseBytes
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
		if err != nil {
			return nil, fmt.Errorf("读取 AI Studio %s 响应: %w", rpc.Method, err)
		}
		if int64(len(raw)) > maxBytes {
			return nil, fmt.Errorf("AI Studio %s 响应超过 %d MiB", rpc.Method, maxBytes>>20)
		}
		response.Body = io.NopCloser(bytes.NewReader(raw))
	}
	return validateRPCResponse(rpc.Method, response)
}

func (c *Client) doProtected(ctx context.Context, request GenerateRequest, body []byte) (*RPCResponse, error) {
	if err := c.pacer.Wait(ctx, request.AccountID); err != nil {
		return nil, err
	}
	rpc := newRPCRequest("GenerateContent", request.AccountID, request.ID, body, true)
	c.applyBenefitTier(rpc.Method, request.AccountID, rpc.Header)
	if request.ImageRoute {
		// 图像请求使用独立于文本预热页的扩展头默认值
		rpc.Header["X-Goog-Ext-519733851-Bin"] = nil
	}
	response, err := c.protected.DoProtected(ctx, request, rpc)
	if err != nil {
		return nil, fmt.Errorf("发送 AI Studio GenerateContent: %w", err)
	}
	return validateRPCResponse("GenerateContent", response)
}

func newRPCRequest(method string, accountID string, requestID string, body []byte, streaming bool) RPCRequest {
	return RPCRequest{
		Method:    method,
		URL:       MakerSuiteRPCBase + method,
		AccountID: accountID,
		RequestID: requestID,
		Header: http.Header{
			"Content-Type": []string{JSONProtobufContentType},
		},
		Body:      body,
		Streaming: streaming,
	}
}

func validateRPCResponse(method string, response *RPCResponse) (*RPCResponse, error) {
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		raw, readErr := readErrorResponseBody(response.Body)
		if readErr != nil {
			return nil, fmt.Errorf("读取 AI Studio %s 错误响应: %w", method, readErr)
		}
		if challenge := DetectChallenge(method, response.StatusCode, response.Header, raw); challenge != nil {
			return nil, challenge
		}
		rpcError := DecodeRPCError(method, response.StatusCode, raw)
		if rpcError.RetryDelay <= 0 {
			value := strings.TrimSpace(response.Header.Get("Retry-After"))
			if delay, err := time.ParseDuration(value + "s"); err == nil && delay > 0 {
				rpcError.RetryDelay = delay
			} else if until, err := http.ParseTime(value); err == nil {
				rpcError.RetryDelay = max(0, time.Until(until))
			}
		}
		return nil, rpcError
	}
	contentType := response.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, JSONProtobufContentType) {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, maxChallengeBody))
		response.Body.Close()
		if challenge := DetectChallenge(method, response.StatusCode, response.Header, raw); challenge != nil {
			return nil, challenge
		}
		return nil, fmt.Errorf("AI Studio %s 返回未识别的 Content-Type %q", method, contentType)
	}
	return response, nil
}

// DecodeRPCError 解析独立状态和流式封装中的上游错误
func DecodeRPCError(method string, statusCode int, raw []byte) *RPCError {
	rpcError := &RPCError{
		Method:     method,
		StatusCode: statusCode,
		Message:    http.StatusText(statusCode),
	}
	value, err := decodeJSONValue(raw)
	if err != nil {
		return rpcError
	}
	root, err := rawArray(value, "$", value)
	if err != nil || len(root) < 2 || isJSONNull(root[1]) {
		return rpcError
	}
	provider, providerPath := root, "$"
	if bytes.HasPrefix(bytes.TrimSpace(root[1]), []byte("[")) {
		provider, err = rawArray(root[1], "$[1]", value)
		if err != nil || len(provider) < 2 {
			return rpcError
		}
		providerPath = "$[1]"
	}
	if code, err := rawInt64(provider[0], providerPath+"[0]", value); err == nil {
		rpcError.Code = code
	}
	if message, err := rawString(provider[1], providerPath+"[1]", value); err == nil && message != "" {
		rpcError.Message = message
	}
	if len(provider) > 2 && !isJSONNull(provider[2]) {
		decodeRPCErrorMetadata(rpcError, provider[2])
	}
	if rpcError.RetryDelay <= 0 {
		rpcError.RetryDelay = messageRetryDelay(rpcError.Message)
	}
	return rpcError
}

// messageRetryDelay 读取错误消息中 retry in 之后的等待时长
func messageRetryDelay(message string) time.Duration {
	_, rest, _ := strings.Cut(strings.ToLower(message), "retry in ")
	value, _, _ := strings.Cut(rest, " ")
	delay, _ := time.ParseDuration(strings.TrimRight(value, "."))
	return delay
}

func decodeRPCErrorMetadata(rpcError *RPCError, raw json.RawMessage) {
	var details [][]json.RawMessage
	if err := json.Unmarshal(raw, &details); err != nil {
		return
	}
	for _, detail := range details {
		if len(detail) < 2 {
			continue
		}
		var typeURL string
		if err := json.Unmarshal(detail[0], &typeURL); err != nil {
			continue
		}
		if typeURL == "type.googleapis.com/google.rpc.RetryInfo" {
			var info []json.RawMessage
			if json.Unmarshal(detail[1], &info) == nil && len(info) > 0 {
				rpcError.RetryDelay = max(rpcError.RetryDelay, decodeRPCRetryDuration(info[0]))
			}
			continue
		}
		if typeURL != "type.googleapis.com/google.rpc.ErrorInfo" {
			continue
		}
		var info []json.RawMessage
		if err := json.Unmarshal(detail[1], &info); err != nil || len(info) < 3 {
			continue
		}
		var metadata [][]string
		if err := json.Unmarshal(info[2], &metadata); err != nil {
			continue
		}
		for _, pair := range metadata {
			if len(pair) < 2 || pair[0] == "" {
				continue
			}
			if rpcError.Metadata == nil {
				rpcError.Metadata = make(map[string]string)
			}
			rpcError.Metadata[pair[0]] = pair[1]
		}
	}
}

// decodeRPCRetryDuration 读取 RetryInfo 中的秒与纳秒
func decodeRPCRetryDuration(raw json.RawMessage) time.Duration {
	var fields []json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) == 0 {
		return 0
	}
	var seconds json.Number
	if json.Unmarshal(fields[0], &seconds) != nil {
		return 0
	}
	delay, err := time.ParseDuration(seconds.String() + "s")
	if err != nil || delay < 0 {
		return 0
	}
	if len(fields) > 1 && !isJSONNull(fields[1]) {
		var nanos int64
		if json.Unmarshal(fields[1], &nanos) != nil || nanos < 0 || nanos >= int64(time.Second) {
			return 0
		}
		delay += time.Duration(nanos)
	}
	return delay
}
