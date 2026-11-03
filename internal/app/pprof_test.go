package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPProfHandlerServesProfiles 验证性能分析端点可访问且包含常见剖析入口
func TestPProfHandlerServesProfiles(t *testing.T) {
	server := httptest.NewServer(pprofHandler())
	defer server.Close()
	response, err := http.Get(server.URL + "/debug/pprof/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", response.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"heap", "goroutine", "profile"} {
		if !strings.Contains(string(body), profile) {
			t.Fatalf("索引页缺少 %s 剖析入口", profile)
		}
	}
}
