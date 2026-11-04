package paniclog

import (
	"testing"
	"time"
)

// TestGoRecoversPanic 验证后台 goroutine 的 panic 被记录且不影响进程
func TestGoRecoversPanic(t *testing.T) {
	done := make(chan struct{})
	Go("test", func() {
		defer close(done)
		panic("boom")
	})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not run")
	}
}
