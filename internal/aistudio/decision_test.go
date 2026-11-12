package aistudio

import (
	"bytes"
	"strings"
	"testing"
)

// TestDecodeCreateDecisionResponseRejectsOversizeBody 验证 CreateDecision 响应超过 8 MiB 上限时被拒绝，而非无界读入内存。
func TestDecodeCreateDecisionResponseRejectsOversizeBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), int(maxDecisionResponseBytes)+1)
	if _, err := DecodeCreateDecisionResponse(bytes.NewReader(body)); err == nil {
		t.Fatal("超过上限的响应应返回错误")
	} else if !strings.Contains(err.Error(), "超过 8 MiB") {
		t.Fatalf("超限错误 = %v, 应包含上限提示", err)
	}
}

// TestDecodeCreateDecisionResponseMinimal 验证最小合法响应仍可正常解码。
func TestDecodeCreateDecisionResponseMinimal(t *testing.T) {
	result, err := DecodeCreateDecisionResponse(strings.NewReader(`[[["q",[0.9,null,null,null,null,[0.5,null,null,[],[]]]]]]`))
	if err != nil {
		t.Fatalf("解码最小响应: %v", err)
	}
	if len(result.Answers) != 1 || result.Answers[0].Key != "q" || result.Answers[0].Confidence != 0.9 || result.Answers[0].Type != "score" {
		t.Fatalf("解码结果 = %+v, 与预期不符", result.Answers)
	}
}
