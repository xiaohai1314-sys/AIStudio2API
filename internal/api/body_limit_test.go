package api

import "testing"

// TestPublicBodyLimit 验证 Gemini JSON 接口使用更小的请求体上限，上传接口保留大文件上限
func TestPublicBodyLimit(t *testing.T) {
	if got := publicBodyLimit("/v1beta/models/gemini-test:generateContent"); got != maxGeminiJSONBodyBytes {
		t.Fatalf("Gemini JSON 上限 = %d, want %d", got, maxGeminiJSONBodyBytes)
	}
	for _, path := range []string{"/upload/v1beta/files", "/v1/chat/completions", "/v1/messages"} {
		if got := publicBodyLimit(path); got != maxPublicBodyBytes {
			t.Fatalf("%s 上限 = %d, want %d", path, got, maxPublicBodyBytes)
		}
	}
}
