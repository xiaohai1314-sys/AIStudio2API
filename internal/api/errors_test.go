package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// TestContentPolicyErrorMapping 覆盖 L2 的分流：输入端拦截（promptFeedback，
// 生成开始前就拒绝输入）是确定性客户端错误，映射为 400 + 明确错误码；
// 输出端拦截（生成中途因内容策略结束）保留上游 a68acad 的 502 可重试
// 语义，重试同一提示词有时能通过。
func TestContentPolicyErrorMapping(t *testing.T) {
	inputErr := &aistudio.ContentPolicyError{Reason: "SAFETY", Message: "blocked by policy", Input: true}

	if status := statusFromError(inputErr); status != http.StatusBadRequest {
		t.Fatalf("输入端内容策略拦截应映射为 400，实际 %d", status)
	}
	if code := openAIErrorCode(inputErr); code != "content_policy" {
		t.Fatalf("输入端内容策略拦截的 OpenAI 错误码应为 content_policy，实际 %q", code)
	}
	if status := geminiErrorStatus(inputErr); status != "INVALID_ARGUMENT" {
		t.Fatalf("输入端内容策略拦截的 Gemini 状态应为 INVALID_ARGUMENT，实际 %q", status)
	}
	if errorType := anthropicErrorType(inputErr); errorType != "invalid_request_error" {
		t.Fatalf("输入端内容策略拦截的 Anthropic 类型应为 invalid_request_error，实际 %q", errorType)
	}

	outputErr := &aistudio.ContentPolicyError{Reason: "SAFETY", Message: "blocked by policy"}
	if status := statusFromError(outputErr); status != http.StatusBadGateway {
		t.Fatalf("输出端内容策略拦截应保留 502，实际 %d", status)
	}
	if code := openAIErrorCode(outputErr); code != "content_policy" {
		t.Fatalf("输出端内容策略拦截的 OpenAI 错误码应为 content_policy，实际 %q", code)
	}
	if status := geminiErrorStatus(outputErr); status != "INTERNAL" {
		t.Fatalf("输出端内容策略拦截的 Gemini 状态应为 INTERNAL，实际 %q", status)
	}
	if errorType := anthropicErrorType(outputErr); errorType != "api_error" {
		t.Fatalf("输出端内容策略拦截的 Anthropic 类型应为 api_error，实际 %q", errorType)
	}

	if status := statusFromError(errors.New("boom")); status != http.StatusBadGateway {
		t.Fatalf("未知错误应保持兜底 502，实际 %d", status)
	}
}
