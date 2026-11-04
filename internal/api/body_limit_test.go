package api

import "testing"

// TestPublicBodyLimit 验证整包解码的 JSON 接口使用更小的请求体上限，上传接口保留大文件上限
func TestPublicBodyLimit(t *testing.T) {
	for _, path := range []string{
		"/v1beta/models/gemini-test:generateContent",
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/responses",
		"/v1/responses/compact",
		"/v1/messages",
		"/v1/messages/count_tokens",
	} {
		if got := publicBodyLimit(path); got != maxJSONBodyBytes {
			t.Fatalf("%s 上限 = %d, want %d", path, got, maxJSONBodyBytes)
		}
	}
	for _, path := range []string{
		"/upload/v1beta/files",
		"/v1/audio/transcriptions",
		"/v1/audio/translations",
		"/v1/images/edits",
		"/v1/files",
	} {
		if got := publicBodyLimit(path); got != maxPublicBodyBytes {
			t.Fatalf("%s 上限 = %d, want %d", path, got, maxPublicBodyBytes)
		}
	}
}
