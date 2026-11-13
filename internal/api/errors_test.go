package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// TestContentPolicyErrorMapping 覆盖 L2：内容策略拦截是确定性的客户端
// 错误，必须映射为 400 + 明确错误码，而不是上游 a68acad 改成的 502
// （502 会让客户端反复重试一个永远会被拦的提示词）。
func TestContentPolicyErrorMapping(t *testing.T) {
	policyErr := &aistudio.ContentPolicyError{Reason: "SAFETY", Message: "blocked by policy"}

	if status := statusFromError(policyErr); status != http.StatusBadRequest {
		t.Fatalf("内容策略拦截应映射为 400，实际 %d", status)
	}
	if code := openAIErrorCode(policyErr); code != "content_policy" {
		t.Fatalf("内容策略拦截的 OpenAI 错误码应为 content_policy，实际 %q", code)
	}
	if status := geminiErrorStatus(policyErr); status != "INVALID_ARGUMENT" {
		t.Fatalf("内容策略拦截的 Gemini 状态应为 INVALID_ARGUMENT，实际 %q", status)
	}
	if errorType := anthropicErrorType(policyErr); errorType != "invalid_request_error" {
		t.Fatalf("内容策略拦截的 Anthropic 类型应为 invalid_request_error，实际 %q", errorType)
	}
	if status := statusFromError(errors.New("boom")); status != http.StatusBadGateway {
		t.Fatalf("未知错误应保持兜底 502，实际 %d", status)
	}
}
