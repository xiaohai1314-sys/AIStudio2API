package api

import (
	"net/http"
	"testing"
)

// benchWriter 丢弃写入内容并按 Flush 语义立即返回，避免基准计入网络与缓冲开销
type benchWriter struct {
	header http.Header
}

func (writer *benchWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = http.Header{}
	}
	return writer.header
}

func (writer *benchWriter) Write(body []byte) (int, error) { return len(body), nil }

func (writer *benchWriter) WriteHeader(int) {}

func (writer *benchWriter) Flush() {}

// BenchmarkWriteChatChunk 测量单个 Chat Completions 流式分块的字段排序、JSON 编码与写出
func BenchmarkWriteChatChunk(b *testing.B) {
	writer := &benchWriter{}
	delta := map[string]any{"content": "Hello, this is a streamed token chunk."}
	b.ReportAllocs()
	for b.Loop() {
		if err := writeChatChunk(writer, "chatcmpl-bench", 1700000000, "gemini-2.5-pro", 0, delta, nil, false); err != nil {
			b.Fatal(err)
		}
	}
}
