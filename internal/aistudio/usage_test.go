package aistudio

import "testing"

// TestExcludeSystemRoleClampsAtZero 验证扣除系统指令 token 后用量不会变成负数
func TestExcludeSystemRoleClampsAtZero(t *testing.T) {
	usage := Usage{
		InputTokens: 10, TotalTokens: 20,
		Details: &UsageDetails{PromptTokensDetails: []ModalityTokenCount{
			{Modality: "TEXT", TokenCount: 5},
			{Modality: "IMAGE", TokenCount: 7},
		}},
	}
	got := excludeSystemRole(usage, 100)
	if got.InputTokens != 0 || got.TotalTokens != 0 {
		t.Fatalf("用量没有归零: input=%d total=%d", got.InputTokens, got.TotalTokens)
	}
	details := got.Details.PromptTokensDetails
	if details[0].TokenCount != 0 {
		t.Fatalf("TEXT 用量没有归零: %d", details[0].TokenCount)
	}
	if details[1].TokenCount != 7 {
		t.Fatalf("其他模态被误改: %d", details[1].TokenCount)
	}
}
