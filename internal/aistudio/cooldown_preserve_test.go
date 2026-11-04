package aistudio

import (
	"testing"
	"time"
)

// TestCooldownNotShortened 验证仍在生效的长冷却不会被后到的短冷却覆盖
func TestCooldownNotShortened(t *testing.T) {
	account := &Account{ID: "user@example.com", Config: AccountConfig{Enabled: true}, State: AccountReady}
	pool := NewAccountPool([]*Account{account}, 1)
	now := time.Now()
	longUntil := now.Add(24 * time.Hour)
	generation := account.modelAccessGeneration
	if err := pool.MarkCooldownIfGeneration(account.ID, "gemini-test", generation, now, longUntil, "每日限额"); err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkCooldownIfGeneration(account.ID, "gemini-test", generation, now.Add(time.Second), now.Add(30*time.Second), "临时失败"); err != nil {
		t.Fatal(err)
	}
	account.runtimeMu.Lock()
	cooldown := account.runtime.Cooldowns["gemini-test"]
	account.runtimeMu.Unlock()
	if !cooldown.Until.Equal(longUntil) || cooldown.Reason != "每日限额" {
		t.Fatalf("冷却被缩短: %+v", cooldown)
	}
}
