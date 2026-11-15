package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

func TestGenerateAttemptPanicEntersFailureAndRetryPath(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	attemptCtx, cancelAttempt := context.WithCancel(requestCtx)
	attempt := &generateAttempt{
		startedAt: time.Now(),
		cancel:    cancelAttempt,
		done:      make(chan struct{}),
	}

	launchGenerateAttempt(attempt, func() {
		panic("synthetic upstream panic")
	})
	select {
	case <-attempt.done:
	case <-time.After(time.Second):
		t.Fatal("attempt did not finish after panic")
	}
	if !errors.Is(attempt.err, errGenerateAttemptPanic) {
		t.Fatalf("attempt error = %v, want non-empty panic error", attempt.err)
	}
	if !strings.Contains(attempt.err.Error(), "synthetic upstream panic") {
		t.Fatalf("attempt error = %q, want panic value", attempt.err)
	}
	if !errors.Is(attemptCtx.Err(), context.Canceled) {
		t.Fatalf("attempt context error = %v, want canceled", attemptCtx.Err())
	}

	failed := false
	winner := (&trackedService{}).awaitFirstEvent(
		requestCtx, aistudio.GenerateRequest{}, "test-model", attempt, nil,
		func(got *generateAttempt, _ bool) {
			failed = true
			if got.err == nil {
				t.Error("failure callback received an empty attempt error")
			}
		},
	)
	if winner != nil || !failed {
		t.Fatalf("panic attempt state: winner=%v failure-routed=%t, want no winner and failure callback", winner, failed)
	}
	if !retryableGenerateAccountError(requestCtx, attempt.err) {
		t.Fatal("panic error was not classified for the bounded account retry path")
	}
}
