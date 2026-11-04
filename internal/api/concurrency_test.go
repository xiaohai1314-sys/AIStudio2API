package api

import (
	"fmt"
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
	handler := publicRateLimitMiddleware("key-a,key-b", 2, 1, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestPublicRateLimiterEvictsOldestWhenFull(t *testing.T) {
	limiter := newPublicRateLimiter(1, 1)
	now := time.Now()
	for index := range maxPublicRateLimitBuckets {
		identity := fmt.Sprintf("peer:%d", index)
		if _, allowed := limiter.allow(identity, now.Add(time.Duration(index)*time.Millisecond)); !allowed {
			t.Fatalf("第 %d 个身份被拒绝", index)
		}
	}
	latest := now.Add(time.Duration(maxPublicRateLimitBuckets) * time.Millisecond)
	if _, allowed := limiter.allow("peer:new", latest); !allowed {
		t.Fatal("达到上限后新身份应当被接受")
	}
	if len(limiter.buckets) != maxPublicRateLimitBuckets {
		t.Fatalf("淘汰后桶数量 = %d", len(limiter.buckets))
	}
	if _, exists := limiter.buckets["peer:0"]; exists {
		t.Fatal("最久未使用的身份应当被淘汰")
	}
}

func TestPublicRateLimiterEvictsRefilledBuckets(t *testing.T) {
	limiter := newPublicRateLimiter(60, 1)
	now := time.Now()
	for index := range maxPublicRateLimitBuckets {
		limiter.allow(fmt.Sprintf("peer:%d", index), now)
	}
	if _, allowed := limiter.allow("peer:new", now.Add(2*time.Second)); !allowed {
		t.Fatal("新身份应当被接受")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("空闲到回满的桶应当全部淘汰，剩余 %d", len(limiter.buckets))
	}
}

// TestPublicRateLimitPerKeyOverride 验证个别 key 使用自己的上限，其他 key 沿用全局上限
func TestPublicRateLimitPerKeyOverride(t *testing.T) {
	handler := publicRateLimitMiddleware("key-a,key-b", 60, 30, map[string]KeyRateLimit{"key-a": {PerMinute: 1}}, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	if response := request("key-a"); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited key-a request = status %d retry-after %q", response.Code, response.Header().Get("Retry-After"))
	}
	for range 2 {
		if response := request("key-b"); response.Code != http.StatusNoContent {
			t.Fatalf("key-b should keep the global limit, got %d", response.Code)
		}
	}
}
