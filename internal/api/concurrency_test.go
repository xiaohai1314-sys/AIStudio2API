package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicConcurrencyLimit(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := publicConcurrencyMiddleware(1, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		first := httptest.NewRecorder()
		handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		firstDone <- first
	}()
	<-entered
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") != "1" {
		t.Fatalf("second request = status %d, retry-after %q", second.Code, second.Header().Get("Retry-After"))
	}
	close(release)
	if first := <-firstDone; first.Code != http.StatusNoContent {
		t.Fatalf("first request = %d", first.Code)
	}
}

func TestPublicRateLimitPerAPIKey(t *testing.T) {
	handler := publicRateLimitMiddleware("key-a,key-b", 2, 1, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(key string) *httptest.ResponseRecorder {
		writer := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		handler.ServeHTTP(writer, req)
		return writer
	}
	if response := request("key-a"); response.Code != http.StatusNoContent {
		t.Fatalf("first key-a request = %d", response.Code)
	}
	if response := request("key-a"); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "30" {
		t.Fatalf("limited key-a request = status %d retry-after %q", response.Code, response.Header().Get("Retry-After"))
	}
	if response := request("key-b"); response.Code != http.StatusNoContent {
		t.Fatalf("key-b request should have a separate bucket, got %d", response.Code)
	}
}

func TestPublicRateLimiterRefills(t *testing.T) {
	limiter := newPublicRateLimiter(60, 1)
	now := time.Now()
	if _, allowed := limiter.allow("key", now); !allowed {
		t.Fatal("initial token was rejected")
	}
	if retryAfter, allowed := limiter.allow("key", now); allowed || retryAfter != 1 {
		t.Fatalf("empty bucket = allowed %t retry-after %d", allowed, retryAfter)
	}
	if _, allowed := limiter.allow("key", now.Add(time.Second)); !allowed {
		t.Fatal("one minute-rate token did not refill after one second")
	}
}
