package aistudio

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReloadStorageStateWithGenerationHoldsSnapshotLocks(t *testing.T) {
	accountID := "user@example.com"
	storage := filepath.Join(t.TempDir(), "storage-state.json")
	initial := StorageState{Cookies: []StateCookie{{Name: "SIDCC", Value: "old", Domain: ".google.com", Path: "/"}}}
	if err := WriteStorageState(storage, initial); err != nil {
		t.Fatal(err)
	}
	account := &Account{
		ID: accountID, State: AccountReady, Config: AccountConfig{Enabled: true, Label: accountID},
		StoragePath: storage, StorageState: initial,
	}
	pool := NewAccountPool([]*Account{account}, 1)
	lease, err := pool.AcquireFor(context.Background(), AccountSelection{AccountID: accountID})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	state, generation, err := lease.reloadStorageStateWithGeneration(func() {
		if lease.operation.TryLock() {
			lease.operation.Unlock()
			t.Error("账户操作锁未覆盖状态快照与认证代际读取")
		}
		if account.storageMu.TryLock() {
			account.storageMu.Unlock()
			t.Error("存储锁未覆盖状态快照与认证代际读取")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Cookies) != 1 || state.Cookies[0].Value != "old" {
		t.Fatalf("快照返回了错误的存储状态: %+v", state.Cookies)
	}
	if generation != lease.AuthGeneration() {
		t.Fatalf("快照认证代际 = %d，当前代际 = %d", generation, lease.AuthGeneration())
	}
}
