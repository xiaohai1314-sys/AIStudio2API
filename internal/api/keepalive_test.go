package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func withKeepaliveInterval(t *testing.T, interval time.Duration) {
	t.Helper()
	previous := nonstreamKeepaliveInterval
	nonstreamKeepaliveInterval = interval
	t.Cleanup(func() { nonstreamKeepaliveInterval = previous })
}

func newKeepaliveServer(enabled bool) *server {
	return &server{config: Config{ResponseModes: func() ResponseModes {
		return ResponseModes{NonstreamKeepalive: enabled}
	}}}
}

type captureResponse struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (writer *captureResponse) Header() http.Header {
	if writer.header == nil {
		writer.header = make(http.Header)
	}
	return writer.header
}

func (writer *captureResponse) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.wroteHeader = true
	writer.status = status
}

func (writer *captureResponse) Write(data []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.body.Write(data)
}

func (writer *captureResponse) Flush() {}

func runKeepalive(enabled bool, request *http.Request, respond func(http.ResponseWriter, *http.Request)) *captureResponse {
	recorder := &captureResponse{}
	server := newKeepaliveServer(enabled)
	writer, stop := server.nonstreamKeepalive(recorder, request)
	defer stop()
	respond(writer, request)
	return recorder
}

func TestNonstreamKeepaliveSlowResponses(t *testing.T) {
	withKeepaliveInterval(t, 20*time.Millisecond)
	slow := 300 * time.Millisecond

	t.Run("default failure keeps status", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		metadata := &accessLogMetadata{}
		request = request.WithContext(context.WithValue(request.Context(), accessLogContextKey{}, metadata))
		response := runKeepalive(false, request, func(writer http.ResponseWriter, _ *http.Request) {
			time.Sleep(slow)
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":"rate limit"}`))
		})
		if !response.wroteHeader || response.status != http.StatusTooManyRequests {
			t.Fatalf("默认保活关闭时慢失败状态 = wrote:%v status:%d body:%q", response.wroteHeader, response.status, response.body.Bytes())
		}
		if metadata.failureStatus != 0 {
			t.Fatalf("未提前提交 200 时不应改写访问日志状态: %d", metadata.failureStatus)
		}
	})

	t.Run("default success", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		response := runKeepalive(false, request, func(writer http.ResponseWriter, _ *http.Request) {
			time.Sleep(slow)
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		})
		if response.status != http.StatusOK || response.body.String() != `{"ok":true}` {
			t.Fatalf("慢成功 = %d %q", response.status, response.body.String())
		}
	})

	t.Run("enabled fast failure keeps status", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		response := runKeepalive(true, request, func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"error":"auth"}`))
		})
		if response.status != http.StatusUnauthorized || !bytes.Contains(response.body.Bytes(), []byte("auth")) {
			t.Fatalf("间隔到达前的认证失败 = %d %q", response.status, response.body.String())
		}
	})

	t.Run("enabled slow failure commits 200", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		metadata := &accessLogMetadata{}
		request = request.WithContext(context.WithValue(request.Context(), accessLogContextKey{}, metadata))
		response := runKeepalive(true, request, func(writer http.ResponseWriter, _ *http.Request) {
			time.Sleep(slow)
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":"rate limit"}`))
		})
		if response.status != http.StatusOK || !bytes.Contains(response.body.Bytes(), []byte(`{"error":"rate limit"}`)) {
			t.Fatalf("显式保活的慢失败 = %d %q", response.status, response.body.String())
		}
		if metadata.failureStatus != http.StatusTooManyRequests {
			t.Fatalf("访问日志状态 = %d", metadata.failureStatus)
		}
	})

	t.Run("enabled slow success", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		response := runKeepalive(true, request, func(writer http.ResponseWriter, _ *http.Request) {
			time.Sleep(slow)
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		})
		if response.status != http.StatusOK || !bytes.Contains(response.body.Bytes(), []byte(`{"ok":true}`)) {
			t.Fatalf("显式保活的慢成功 = %d %q", response.status, response.body.String())
		}
	})
}

func TestNonstreamKeepaliveCancelBeforeCommit(t *testing.T) {
	withKeepaliveInterval(t, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	response := runKeepalive(true, request, func(_ http.ResponseWriter, current *http.Request) {
		cancel()
		<-current.Context().Done()
	})
	if response.wroteHeader || response.body.Len() != 0 {
		t.Fatalf("取消后不应提交保活响应: wrote=%v body=%q", response.wroteHeader, response.body.String())
	}
}
