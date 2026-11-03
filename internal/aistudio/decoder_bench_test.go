package aistudio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// benchGeneratePayload 构造上游生成流：field 1 为 repeated 分块数组（含稀疏空位），field 2 为 code 0 的流尾状态
func benchGeneratePayload(chunks, textSize int) []byte {
	text := strings.Repeat("流", textSize/3)
	var builder strings.Builder
	builder.WriteString("[[")
	for index := 0; index < chunks; index++ {
		if index > 0 {
			builder.WriteString(",")
		}
		if index%5 == 4 {
			continue
		}
		fmt.Fprintf(&builder, `{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]},"index":0}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":%d,"totalTokenCount":%d}}`, text, index, 100+index)
	}
	builder.WriteString("], [0]]")
	return []byte(builder.String())
}

func benchDecodeGenerateItems(b *testing.B, payload []byte) {
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		if err := decodeGenerateItems(bytes.NewReader(payload), func(json.RawMessage) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecodeGenerateItemsText 模拟短文本增量流：200 个分块、每块约 48 字节
func BenchmarkDecodeGenerateItemsText(b *testing.B) {
	benchDecodeGenerateItems(b, benchGeneratePayload(200, 48))
}

// BenchmarkDecodeGenerateItemsLongText 模拟长文本分块：20 个分块、每块约 4 KiB
func BenchmarkDecodeGenerateItemsLongText(b *testing.B) {
	benchDecodeGenerateItems(b, benchGeneratePayload(20, 4096))
}
