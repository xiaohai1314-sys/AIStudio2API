package aistudio

import (
	"testing"
	"time"
)

// TestRecordRiskExposedInStatus 验证风控计数按类别累计并出现在账户状态中
func TestRecordRiskExposedInStatus(t *testing.T) {
	account := &Account{ID: "user@example.com", Config: AccountConfig{Enabled: true}, State: AccountReady}
	pool := NewAccountPool([]*Account{account}, 1)
	pool.RecordRisk(account.ID, RiskForbidden)
	pool.RecordRisk(account.ID, RiskForbidden)
	pool.RecordRisk(account.ID, RiskChallenge)
	pool.RecordRisk(account.ID, RiskRateLimited)
	statuses := pool.Status()
	if len(statuses) != 1 {
		t.Fatalf("状态数量 = %d", len(statuses))
	}
	risk := statuses[0].Risk
	if risk.Forbidden != 2 || risk.Challenge != 1 || risk.RateLimited != 1 {
		t.Fatalf("risk = %+v", risk)
	}
	if risk.LastAt.IsZero() {
		t.Fatal("最近命中时间没有记录")
	}
}

// TestRecordRiskResetsAfterWindow 验证超过统计窗口后计数从零开始
func TestRecordRiskResetsAfterWindow(t *testing.T) {
	account := &Account{ID: "user@example.com", Config: AccountConfig{Enabled: true}, State: AccountReady}
	pool := NewAccountPool([]*Account{account}, 1)
	pool.RecordRisk(account.ID, RiskForbidden)
	pool.mu.Lock()
	pool.risk[account.ID].since = time.Now().Add(-riskWindow - time.Minute)
	pool.mu.Unlock()
	risk := pool.RecordRisk(account.ID, RiskForbidden)
	if risk.Forbidden != 1 || risk.Challenge != 0 {
		t.Fatalf("窗口重置后 risk = %+v", risk)
	}
}

// TestRecordRiskUnknownAccount 验证不存在的账户不计数
func TestRecordRiskUnknownAccount(t *testing.T) {
	pool := NewAccountPool(nil, 1)
	if risk := pool.RecordRisk("missing", RiskForbidden); !risk.IsZero() {
		t.Fatalf("risk = %+v", risk)
	}
}
