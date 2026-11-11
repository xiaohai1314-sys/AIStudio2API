package aistudio

import (
	"errors"
	"sync"
	"testing"
)

// TestAccountLeaseRetainEndIsIdempotent 验证同一个 Retain 结束函数重复调用不会重复减少 holds。
func TestAccountLeaseRetainEndIsIdempotent(t *testing.T) {
	lease := &AccountLease{}
	end := lease.Retain()

	if got := lease.holds; got != 1 {
		t.Fatalf("Retain 后 holds = %d, want 1", got)
	}
	if err := end(); err != nil {
		t.Fatalf("第一次结束租约: %v", err)
	}
	if err := end(); err != nil {
		t.Fatalf("重复结束租约: %v", err)
	}
	if got := lease.holds; got != 0 {
		t.Fatalf("结束函数重复调用后 holds = %d, want 0", got)
	}
}

func TestAccountLeaseRetainEndPreservesErrorOnRepeatedCall(t *testing.T) {
	wantErr := errors.New("release failed")
	lease := &AccountLease{err: wantErr}
	end := lease.Retain()

	for range 2 {
		if err := end(); !errors.Is(err, wantErr) {
			t.Fatalf("结束函数返回错误 %v, want %v", err, wantErr)
		}
	}
}

// TestAccountLeaseRetainEndIsConcurrentIdempotent 验证并发重复调用同一个结束函数仍只生效一次。
func TestAccountLeaseRetainEndIsConcurrentIdempotent(t *testing.T) {
	lease := &AccountLease{}
	end := lease.Retain()

	const callers = 16
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			if err := end(); err != nil {
				t.Errorf("并发结束租约: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := lease.holds; got != 0 {
		t.Fatalf("并发重复调用结束函数后 holds = %d, want 0", got)
	}
}
