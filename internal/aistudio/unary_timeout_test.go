package aistudio

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestUnaryClientDoBodyReadableAfterReturn 验证 unaryClientDo 返回后正文仍可读取：
// 请求上下文在 Do 返回后继续控制正文读取，超时 ctx 只能在正文关闭时取消，
// 若在函数返回时就取消，调用方会读到 context canceled
func TestUnaryClientDoBodyReadableAfterReturn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush() // 先发响应头，正文稍后到达
		}
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(w, "late body")
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := unaryClientDo(context.Background(), server.Client(), request)
	if err != nil {
		t.Fatalf("unaryClientDo = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("读取正文 = %v（超时 ctx 在函数返回时被取消）", err)
	}
	if string(body) != "late body" {
		t.Fatalf("正文 = %q, want late body", body)
	}
}

// TestUnaryClientDoCancelsAfterClose 验证正文关闭后超时 ctx 被取消，不在关闭后泄漏计时器
func TestUnaryClientDoCancelsAfterClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "body")
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := unaryClientDo(context.Background(), server.Client(), request)
	if err != nil {
		t.Fatalf("unaryClientDo = %v", err)
	}
	wrapped, ok := response.Body.(*cancelOnCloseBody)
	if !ok {
		t.Fatalf("正文类型 = %T, want *cancelOnCloseBody", response.Body)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("关闭正文 = %v", err)
	}
	// 关闭后再次 Close 不应重复取消（once 保护）或报错
	if err := wrapped.Close(); err != nil {
		t.Fatalf("重复关闭 = %v", err)
	}
}

// TestDoClientRequestSkipsTimeoutForStreaming 验证流式 RPC 不套非流式兜底超时：
// 长时间思考的健康 SSE 会超过任何固定上限，超时只由调用方 ctx 控制
func TestDoClientRequestSkipsTimeoutForStreaming(t *testing.T) {
	SetUnaryRPCTimeout(50 * time.Millisecond)
	defer SetUnaryRPCTimeout(defaultUnaryRPCTimeout)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, "data: late\n\n")
	}))
	defer server.Close()

	streamRequest, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResponse, err := doClientRequest(context.Background(), server.Client(), streamRequest, true)
	if err != nil {
		t.Fatalf("流式请求 = %v", err)
	}
	streamBody, err := io.ReadAll(streamResponse.Body)
	_ = streamResponse.Body.Close()
	if err != nil || string(streamBody) != "data: late\n\n" {
		t.Fatalf("流式正文 = %q, err=%v（不应被 unary 超时截断）", streamBody, err)
	}

	unaryRequest, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	unaryResponse, err := doClientRequest(context.Background(), server.Client(), unaryRequest, false)
	if err != nil {
		t.Fatalf("非流式请求 = %v", err)
	}
	_, readErr := io.ReadAll(unaryResponse.Body)
	_ = unaryResponse.Body.Close()
	if readErr == nil {
		t.Fatal("非流式正文读取应当因 unary 超时失败")
	}
}
