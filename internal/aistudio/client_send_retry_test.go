package aistudio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// fakeSendTransport 是 send 重试测试用的传输层：前 failTimes 次调用返回 failErr，之后返回成功响应
type fakeSendTransport struct {
	calls     atomic.Int32
	failTimes int32
	failErr   error
}

func (t *fakeSendTransport) Do(ctx context.Context, rpc RPCRequest) (*RPCResponse, error) {
	if t.calls.Add(1) <= t.failTimes {
		return nil, t.failErr
	}
	header := make(http.Header)
	header.Set("Content-Type", JSONProtobufContentType)
	return &RPCResponse{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(`[[null]]`)),
	}, nil
}

// newSendTestClient 返回用 fakeSendTransport 的 Client，并把重试等待缩短以便测试快速完成
func newSendTestClient(t *testing.T, transport *fakeSendTransport) *Client {
	t.Helper()
	orig := unaryRetryBackoff
	unaryRetryBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { unaryRetryBackoff = orig })
	return NewClient(ClientOptions{Transport: transport})
}

func TestIsTransientTransportError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"EOF", io.EOF, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"broken pipe", syscall.EPIPE, true},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"generic error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientTransportError(tc.err); got != tc.want {
				t.Fatalf("isTransientTransportError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSendRetriesUnaryOnEOF(t *testing.T) {
	transport := &fakeSendTransport{failTimes: 2, failErr: io.EOF}
	client := newSendTestClient(t, transport)
	rpc := RPCRequest{
		Method:    "GenerateAccessToken",
		AccountID: "test-account",
		Header:    make(http.Header),
		Body:      []byte(`["users/me"]`),
	}
	if _, err := client.send(context.Background(), rpc); err != nil {
		t.Fatalf("send error = %v, want nil after retry", err)
	}
	if got := transport.calls.Load(); got != 3 {
		t.Fatalf("transport calls = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestSendGivesUpAfterUnaryRetries(t *testing.T) {
	transport := &fakeSendTransport{failTimes: 10, failErr: io.EOF}
	client := newSendTestClient(t, transport)
	rpc := RPCRequest{
		Method:    "GenerateAccessToken",
		AccountID: "test-account",
		Header:    make(http.Header),
		Body:      []byte(`["users/me"]`),
	}
	if _, err := client.send(context.Background(), rpc); err == nil {
		t.Fatal("send error = nil, want EOF after retries exhausted")
	}
	if got := transport.calls.Load(); got != int32(unaryRetryAttempts) {
		t.Fatalf("transport calls = %d, want %d", got, unaryRetryAttempts)
	}
}

func TestSendDoesNotRetryNonTransientError(t *testing.T) {
	transport := &fakeSendTransport{failTimes: 10, failErr: errors.New("boom")}
	client := newSendTestClient(t, transport)
	rpc := RPCRequest{
		Method:    "GenerateAccessToken",
		AccountID: "test-account",
		Header:    make(http.Header),
		Body:      []byte(`["users/me"]`),
	}
	if _, err := client.send(context.Background(), rpc); err == nil {
		t.Fatal("send error = nil, want boom")
	}
	if got := transport.calls.Load(); got != 1 {
		t.Fatalf("transport calls = %d, want 1 (no retry on non-transient error)", got)
	}
}

func TestSendDoesNotRetryStreamingOnEOF(t *testing.T) {
	transport := &fakeSendTransport{failTimes: 10, failErr: io.EOF}
	client := newSendTestClient(t, transport)
	rpc := RPCRequest{
		Method:    "GenerateContent",
		AccountID: "test-account",
		Header:    make(http.Header),
		Body:      []byte(`[]`),
		Streaming: true,
	}
	if _, err := client.send(context.Background(), rpc); err == nil {
		t.Fatal("send error = nil, want EOF")
	}
	if got := transport.calls.Load(); got != 1 {
		t.Fatalf("transport calls = %d, want 1 (streaming never retries)", got)
	}
}
