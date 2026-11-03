package aistudio

import "testing"

// TestPauseAccountPersists 验证暂停账户进入需人工检查状态，只有显式恢复才回到可调度
func TestPauseAccountPersists(t *testing.T) {
	account := &Account{ID: "user@example.com", Config: AccountConfig{Enabled: true}, State: AccountReady}
	pool := NewAccountPool([]*Account{account}, 1)
	if err := pool.PauseAccount(account.ID, "认证失败，已暂停等待人工检查"); err != nil {
		t.Fatalf("PauseAccount() = %v", err)
	}
	if state, _ := pool.AccountState(account.ID); state != AccountAuthRequired {
		t.Fatalf("暂停后状态 = %s, want %s", state, AccountAuthRequired)
	}
	if !account.authPaused {
		t.Fatal("暂停标记没有写入内存账户")
	}
	if err := pool.MarkReady(account.ID); err != nil {
		t.Fatalf("MarkReady() = %v", err)
	}
	if state, _ := pool.AccountState(account.ID); state != AccountReady {
		t.Fatalf("恢复后状态 = %s, want %s", state, AccountReady)
	}
	if account.authPaused {
		t.Fatal("恢复后暂停标记没有清除")
	}
}
