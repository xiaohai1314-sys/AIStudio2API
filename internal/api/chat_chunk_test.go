package api

import (
	"encoding/json"
	"maps"
	"net/http"
	"testing"
)

// sseCaptureWriter 记录写入的完整 SSE 帧
type sseCaptureWriter struct {
	header http.Header
	body   []byte
}

func (writer *sseCaptureWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = http.Header{}
	}
	return writer.header
}

func (writer *sseCaptureWriter) Write(body []byte) (int, error) {
	writer.body = append(writer.body, body...)
	return len(body), nil
}

func (writer *sseCaptureWriter) WriteHeader(int) {}

func (writer *sseCaptureWriter) Flush() {}

// officialChatChunk 按通用字段排序路径生成同样的 chunk，用于校验直编码结果
func officialChatChunk(id string, created int64, model string, index int, delta map[string]any, finish *string, includeUsage bool) any {
	choice := map[string]any{"index": index, "delta": delta, "finish_reason": finish}
	if reason, ok := delta["provider_finish_reason"]; ok {
		delete(delta, "provider_finish_reason")
		choice["provider_finish_reason"] = reason
	}
	chunk := map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []any{choice},
	}
	if includeUsage {
		chunk["usage"] = nil
	}
	return officialOrder("chat", chunk)
}

func TestWriteChatChunkMatchesOfficialOrder(t *testing.T) {
	finish := "stop"
	cases := []struct {
		name    string
		delta   map[string]any
		finish  *string
		include bool
	}{
		{name: "首帧", delta: map[string]any{"role": "assistant", "content": ""}},
		{name: "正文", delta: map[string]any{"content": "hi"}},
		{name: "推理", delta: map[string]any{"reasoning_content": "思考"}},
		{name: "工具调用", delta: map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call"}}}},
		{name: "结束", delta: map[string]any{}, finish: &finish},
		{name: "结束与用量", delta: map[string]any{"provider_finish_reason": "STOP"}, finish: &finish, include: true},
		{name: "多字段乱序", delta: map[string]any{
			"content": "a", "role": "assistant", "annotations": []any{}, "audio": map[string]any{"id": "x"},
		}},
		{name: "空 delta", delta: map[string]any{}},
		{name: "转义字符", delta: map[string]any{"content": "a\"b\\c<d>&e\n\t 中"}},
		{name: "控制字符", delta: map[string]any{"content": "\x00\x1f"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			writer := &sseCaptureWriter{}
			if err := writeChatChunk(writer, "chatcmpl-1", 1700000000, "gemini-2.5-pro", 0, maps.Clone(test.delta), test.finish, test.include); err != nil {
				t.Fatalf("直编码失败: %v", err)
			}
			expected, err := json.Marshal(officialChatChunk("chatcmpl-1", 1700000000, "gemini-2.5-pro", 0, test.delta, test.finish, test.include))
			if err != nil {
				t.Fatalf("通用路径编码失败: %v", err)
			}
			want := "data: " + string(expected) + "\n\n"
			if string(writer.body) != want {
				t.Fatalf("输出不一致\n得到 %s\n期望 %s", writer.body, want)
			}
		})
	}
}

// BenchmarkWriteChatChunkOfficialOrder 测量通用字段排序路径，用于对比直编码的开销
func BenchmarkWriteChatChunkOfficialOrder(b *testing.B) {
	writer := &benchWriter{}
	delta := map[string]any{"content": "Hello, this is a streamed token chunk."}
	b.ReportAllocs()
	for b.Loop() {
		if err := writeSSE(writer, "", officialChatChunk("chatcmpl-bench", 1700000000, "gemini-2.5-pro", 0, delta, nil, false)); err != nil {
			b.Fatal(err)
		}
	}
}
