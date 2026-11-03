package aistudio

import (
	"fmt"
	"testing"
)

// TestEligibleAccountsMatchesStatus 验证轻量计数与账户状态切片给出的可用账户数一致
func TestEligibleAccountsMatchesStatus(t *testing.T) {
	accounts := []*Account{
		{ID: "ready@example.com", Config: AccountConfig{Enabled: true}, State: AccountReady},
		{ID: "busy@example.com", Config: AccountConfig{Enabled: true}, State: AccountReady, active: 1},
		{ID: "disabled@example.com", Config: AccountConfig{Enabled: false}, State: AccountReady},
		{ID: "cooldown@example.com", Config: AccountConfig{Enabled: true}, State: AccountCooldown},
		{ID: "auth@example.com", Config: AccountConfig{Enabled: true}, State: AccountAuthRequired},
	}
	pool := NewAccountPool(accounts, 1)
	want := 0
	for _, status := range pool.Status() {
		if status.Enabled && (status.State == AccountReady || status.State == AccountBusy) {
			want++
		}
	}
	if want != 2 {
		t.Fatalf("测试数据应给出 2 个可用账户，实际 %d", want)
	}
	if got := pool.EligibleAccounts(); got != want {
		t.Fatalf("EligibleAccounts() = %d, want %d", got, want)
	}
}

// benchmarkAccountPool 构造带模型列表的账户池，用于测量估算换号次数的开销
func benchmarkAccountPool(accounts int) *AccountPool {
	models := make([]Model, 40)
	for index := range models {
		models[index] = Model{ID: fmt.Sprintf("model-%02d", index)}
	}
	list := make([]*Account, 0, accounts)
	for index := range accounts {
		list = append(list, &Account{
			ID:     fmt.Sprintf("user%d@example.com", index),
			Config: AccountConfig{Enabled: true}, State: AccountReady, Models: models,
		})
	}
	return NewAccountPool(list, 2)
}

// BenchmarkEligibleAccounts 测量每次生成请求估算换号次数的开销
func BenchmarkEligibleAccounts(b *testing.B) {
	pool := benchmarkAccountPool(64)
	b.ReportAllocs()
	for b.Loop() {
		pool.EligibleAccounts()
	}
}

// BenchmarkEligibleAccountsFromStatus 测量改动前逐账户构建并排序模型列表的开销
func BenchmarkEligibleAccountsFromStatus(b *testing.B) {
	pool := benchmarkAccountPool(64)
	b.ReportAllocs()
	for b.Loop() {
		eligible := 0
		for _, status := range pool.Status() {
			if status.Enabled && (status.State == AccountReady || status.State == AccountBusy) {
				eligible++
			}
		}
		_ = eligible
	}
}
