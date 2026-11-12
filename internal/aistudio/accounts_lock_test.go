package aistudio

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

// TestLockRuntimeStateBackgroundContextHasTimeout 验证无 deadline 的上下文锁定账户运行状态时有兜底上限：
// 锁被其他持有者占住时不会无限等待，持有者释放后可正常加锁。
func TestLockRuntimeStateBackgroundContextHasTimeout(t *testing.T) {
	dir := t.TempDir()
	account := &Account{Directory: filepath.Join(dir, "acct"), RuntimePath: "runtime-state.json"}
	lockPath := filepath.Join(dir, ".leases", "acct.runtime.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatalf("创建锁目录: %v", err)
	}

	holder := flock.New(lockPath)
	held, err := holder.TryLockContext(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("持有测试锁: %v", err)
	}
	if !held {
		t.Fatal("持有测试锁失败")
	}
	defer func() { _ = holder.Unlock() }()

	oldTimeout := runtimeLockTimeout
	runtimeLockTimeout = 200 * time.Millisecond
	defer func() { runtimeLockTimeout = oldTimeout }()

	start := time.Now()
	if _, err := lockRuntimeState(context.Background(), account); err == nil {
		t.Fatalf("锁被占住时 lockRuntimeState 应返回错误")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("无 deadline 的加锁等待 %v, 超过兜底上限预期", elapsed)
	}

	if err := holder.Unlock(); err != nil {
		t.Fatalf("释放测试锁: %v", err)
	}
	locked, err := lockRuntimeState(context.Background(), account)
	if err != nil {
		t.Fatalf("锁释放后 lockRuntimeState: %v", err)
	}
	if locked == nil {
		t.Fatal("锁释放后 lockRuntimeState 返回了空锁")
	}
	if err := locked.Unlock(); err != nil {
		t.Fatalf("释放运行状态锁: %v", err)
	}
}
