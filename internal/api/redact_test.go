package api

import "testing"

// TestRedactBody 验证凭据字段与查询参数被替换，普通内容保持不变
func TestRedactBody(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"api key", `{"model":"x","api_key":"sk-123"}`, `{"model":"x","api_key":"[redacted]"}`},
		{"token", `{"token":"abc","text":"hi"}`, `{"token":"[redacted]","text":"hi"}`},
		{"sse", `data: {"authorization":"Bearer secret"}`, `data: {"authorization":"[redacted]"}`},
		{"query", `https://example.com/v1?key=sk-1&x=2`, `https://example.com/v1?key=[redacted]&x=2`},
		{"password", `{"password":"p@ss"}`, `{"password":"[redacted]"}`},
		{"plain", `{"model":"x","text":"hello"}`, `{"model":"x","text":"hello"}`},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactBody(tc.in); got != tc.want {
				t.Fatalf("redactBody(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRedactBodyIdempotent 验证已脱敏内容再次处理不会改变
func TestRedactBodyIdempotent(t *testing.T) {
	redacted := redactBody(`{"api_key":"sk-123","text":"hi"}`)
	if again := redactBody(redacted); again != redacted {
		t.Fatalf("二次脱敏 = %q, want %q", again, redacted)
	}
}
