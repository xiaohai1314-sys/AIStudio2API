package aistudio

import (
	"io"
	"strings"
	"testing"
)

// TestSparseJSONReaderNormalize 验证稀疏数组空位补成 null，字符串内的逗号与方括号保持原样
func TestSparseJSONReaderNormalize(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"空位", `[[1,,2],[0]]`, `[[1,null,2],[0]]`},
		{"连续空位", `[[1,,,2],[0]]`, `[[1,null,null,2],[0]]`},
		{"尾随逗号", `[1,]`, `[1,null]`},
		{"字符串内符号", `["a,,]","b,c"]`, `["a,,]","b,c"]`},
		{"转义引号", `["a\"b,c"]`, `["a\"b,c"]`},
		{"空白不影响", `[ 1 , , 2 ]`, `[ 1 , null, 2 ]`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output, err := io.ReadAll(newSparseJSONReader(strings.NewReader(test.input)))
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != test.want {
				t.Fatalf("输出 = %q，期望 %q", output, test.want)
			}
		})
	}
}

// TestSparseJSONReaderTinyBuffer 验证一字节目标缓冲下扩展字节跨多次读取正确输出
func TestSparseJSONReaderTinyBuffer(t *testing.T) {
	reader := newSparseJSONReader(strings.NewReader(`[[1,,2],[0]]`))
	var output strings.Builder
	buffer := make([]byte, 1)
	for {
		count, err := reader.Read(buffer)
		output.Write(buffer[:count])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if want := `[[1,null,2],[0]]`; output.String() != want {
		t.Fatalf("输出 = %q，期望 %q", output.String(), want)
	}
}
