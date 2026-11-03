package api

import (
	"testing"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// TestOpenAIFinishReason 验证 Chat 与 Responses 的结束原因映射
func TestOpenAIFinishReason(t *testing.T) {
	cases := []struct {
		reason   string
		hasTools bool
		want     string
	}{
		{reason: "max_tokens", want: "length"},
		{reason: "continuation", want: "length"},
		{reason: "stop_sequence", want: "stop"},
		{reason: "safety", want: "content_filter"},
		{reason: "recitation", want: "content_filter"},
		{reason: "provider_malformed", want: "content_filter"},
		{reason: "", want: "stop"},
		{reason: "", hasTools: true, want: "tool_calls"},
		{reason: "stop", hasTools: true, want: "tool_calls"},
	}
	for _, test := range cases {
		if got := openAIFinishReason(test.reason, test.hasTools); got != test.want {
			t.Fatalf("openAIFinishReason(%q, %t) = %q, want %q", test.reason, test.hasTools, got, test.want)
		}
	}
}

// TestChatFinishReasonLegacyFunctions 验证旧式 functions 的工具调用结束原因为 function_call
func TestChatFinishReasonLegacyFunctions(t *testing.T) {
	result := generationResult{toolCalls: []aistudio.FunctionCall{{Name: "lookup"}}}
	if got := chatFinishReason(result, []openAITool{{legacy: false}}); got != "tool_calls" {
		t.Fatalf("新式工具结束原因 = %q, want tool_calls", got)
	}
	if got := chatFinishReason(result, []openAITool{{legacy: true}}); got != "function_call" {
		t.Fatalf("旧式工具结束原因 = %q, want function_call", got)
	}
	if got := chatFinishReason(generationResult{finishReason: "max_tokens"}, nil); got != "length" {
		t.Fatalf("无工具调用时结束原因 = %q, want length", got)
	}
}

// TestAnthropicStop 验证 Anthropic 的 stop_reason 映射
func TestAnthropicStop(t *testing.T) {
	cases := []struct {
		reason   string
		hasTools bool
		sequence string
		want     string
		wantSeq  *string
	}{
		{reason: "max_tokens", want: "max_tokens"},
		{reason: "continuation", want: "max_tokens"},
		{reason: "compaction", want: "compaction"},
		{reason: "safety", want: "refusal"},
		{reason: "", hasTools: true, want: "tool_use"},
		{reason: "", want: "end_turn"},
		{reason: "stop_sequence", sequence: "\n\n", want: "stop_sequence", wantSeq: pointerTo("\n\n")},
	}
	for _, test := range cases {
		got, sequence := anthropicStop(test.reason, test.hasTools, test.sequence)
		if got != test.want {
			t.Fatalf("anthropicStop(%q) = %q, want %q", test.reason, got, test.want)
		}
		switch {
		case test.wantSeq == nil && sequence != nil:
			t.Fatalf("anthropicStop(%q) 不应返回停止序列", test.reason)
		case test.wantSeq != nil && (sequence == nil || *sequence != *test.wantSeq):
			t.Fatalf("anthropicStop(%q) 停止序列 = %v, want %q", test.reason, sequence, *test.wantSeq)
		}
	}
}

// TestGeminiFinishReason 验证 Gemini 的 finishReason 映射
func TestGeminiFinishReason(t *testing.T) {
	cases := map[string]string{
		"":                        "STOP",
		" stop ":                  "STOP",
		"max_tokens":              "MAX_TOKENS",
		"length":                  "MAX_TOKENS",
		"safety":                  "SAFETY",
		"blocklist":               "BLOCKLIST",
		"malformed_function_call": "MALFORMED_FUNCTION_CALL",
		"continuation":            "CONTINUATION",
		"provider_not_a_finish":   "NOT_A_FINISH",
		"provider_bad-value":      "OTHER",
		"unknown":                 "OTHER",
	}
	for reason, want := range cases {
		if got := geminiFinishReason(reason); got != want {
			t.Fatalf("geminiFinishReason(%q) = %q, want %q", reason, got, want)
		}
	}
}

// TestUsageMapping 验证三种协议的用量映射
func TestUsageMapping(t *testing.T) {
	usage := &aistudio.Usage{
		InputTokens: 10, ToolTokens: 2, OutputTokens: 20, ReasoningTokens: 5,
		CachedTokens: 3, TotalTokens: 37,
	}
	chat := openAIUsage(usage)
	if chat["prompt_tokens"] != int64(12) || chat["completion_tokens"] != int64(25) || chat["total_tokens"] != int64(37) {
		t.Fatalf("Chat 用量 = %v", chat)
	}
	prompt := chat["prompt_tokens_details"].(map[string]any)
	if prompt["cached_tokens"] != int64(3) {
		t.Fatalf("Chat 缓存用量 = %v", prompt)
	}
	completion := chat["completion_tokens_details"].(map[string]any)
	if completion["reasoning_tokens"] != int64(5) {
		t.Fatalf("Chat 推理用量 = %v", completion)
	}

	anthropic := anthropicUsage(usage)
	if anthropic["input_tokens"] != int64(9) || anthropic["cache_read_input_tokens"] != int64(3) || anthropic["output_tokens"] != int64(25) {
		t.Fatalf("Anthropic 用量 = %v", anthropic)
	}

	gemini := geminiUsage(usage)
	if gemini["promptTokenCount"] != int64(10) || gemini["candidatesTokenCount"] != int64(20) || gemini["thoughtsTokenCount"] != int64(5) {
		t.Fatalf("Gemini 用量 = %v", gemini)
	}
	if _, exists := gemini["outputTokenCount"]; exists {
		t.Fatalf("上游没有的字段不应输出: %v", gemini)
	}
}

// TestSumUsage 验证多次生成的用量累加与模态细分的处理
func TestSumUsage(t *testing.T) {
	if sumUsage([]generationResult{{}, {}}) != nil {
		t.Fatal("均无用量时应返回 nil")
	}
	single := sumUsage([]generationResult{{usage: &aistudio.Usage{InputTokens: 1, OutputTokens: 2, Details: &aistudio.UsageDetails{}}}})
	if single == nil || single.InputTokens != 1 || single.Details == nil {
		t.Fatalf("单次生成应保留模态细分: %+v", single)
	}
	total := sumUsage([]generationResult{
		{usage: &aistudio.Usage{InputTokens: 1, OutputTokens: 2, CachedTokens: 3, ReasoningTokens: 4, ToolTokens: 5}},
		{},
		{usage: &aistudio.Usage{InputTokens: 10, OutputTokens: 20, CachedTokens: 30, ReasoningTokens: 40, ToolTokens: 50}},
	})
	if total == nil || total.InputTokens != 11 || total.OutputTokens != 22 || total.CachedTokens != 33 || total.ReasoningTokens != 44 || total.ToolTokens != 55 {
		t.Fatalf("多次生成用量 = %+v", total)
	}
	if total.Details != nil {
		t.Fatal("多次生成不应保留模态细分")
	}
}

func pointerTo(value string) *string {
	return &value
}
