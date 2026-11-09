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

func TestRunWithDoneWaitsForPanicRecovery(t *testing.T) {
	recoveryStarted := make(chan struct{})
	releaseRecovery := make(chan struct{})
	done := make(chan struct{})
	go runWithDone("test", func() { close(done) }, func(string) {
		if recovered := recover(); recovered == nil {
			t.Error("panic recovery callback did not receive panic")
			return
		}
		close(recoveryStarted)
		<-releaseRecovery
	}, func() { panic("boom") })

	select {
	case <-recoveryStarted:
	case <-time.After(time.Second):
		t.Fatal("panic recovery callback did not start")
	}
	select {
	case <-done:
		t.Fatal("done callback ran before panic recovery completed")
	default:
	}
	close(releaseRecovery)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("done callback did not run after panic recovery")
	}
}
