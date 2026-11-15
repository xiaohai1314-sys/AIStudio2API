package aistudio

import "testing"

func TestRunAssistantWithCleanupRecoversBodyPanic(t *testing.T) {
	cleanupCalled := false
	runAssistantWithCleanup("test:assistantBody", func() {
		panic("assistant body panic")
	}, func() {
		cleanupCalled = true
	})
	if !cleanupCalled {
		t.Fatal("cleanup callback was not run after the body panicked")
	}
}

func TestRunAssistantWithCleanupRecoversCleanupPanic(t *testing.T) {
	bodyCalled := false
	runAssistantWithCleanup("test:assistantCleanup", func() {
		bodyCalled = true
	}, func() {
		panic("assistant cleanup panic")
	})
	if !bodyCalled {
		t.Fatal("assistant body was not run before cleanup")
	}
}
