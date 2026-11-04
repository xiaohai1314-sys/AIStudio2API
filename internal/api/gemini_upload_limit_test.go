package api

import (
	"errors"
	"strings"
	"testing"
)

// TestGeminiUploadsLimitSessions 验证会话数达到上限后拒绝新会话，丢弃后可以继续创建
func TestGeminiUploadsLimitSessions(t *testing.T) {
	uploads := newGeminiUploads()
	uploads.maxSessions = 1
	first := &geminiUploadSession{}
	id, err := uploads.start(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uploads.start(&geminiUploadSession{}); !errors.Is(err, errGeminiUploadCapacity) {
		t.Fatalf("second start = %v", err)
	}
	first.mu.Lock()
	uploads.discard(id, first)
	first.mu.Unlock()
	if _, err := uploads.start(&geminiUploadSession{}); err != nil {
		t.Fatalf("start after discard = %v", err)
	}
}

// TestGeminiUploadsLimitBufferedBytes 验证所有会话共享临时空间上限，失败时归还已占用的额度
func TestGeminiUploadsLimitBufferedBytes(t *testing.T) {
	uploads := newGeminiUploads()
	uploads.maxBuffered = 8
	first := &geminiUploadSession{}
	firstID, err := uploads.start(first)
	if err != nil {
		t.Fatal(err)
	}
	first.mu.Lock()
	err = bufferGeminiUpload(uploads, first, strings.NewReader("123456"))
	first.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	second := &geminiUploadSession{}
	secondID, err := uploads.start(second)
	if err != nil {
		t.Fatal(err)
	}
	second.mu.Lock()
	err = bufferGeminiUpload(uploads, second, strings.NewReader("123"))
	second.mu.Unlock()
	if !errors.Is(err, errGeminiUploadCapacity) {
		t.Fatalf("second buffer = %v", err)
	}
	second.mu.Lock()
	uploads.discard(secondID, second)
	second.mu.Unlock()

	uploads.mu.Lock()
	buffered := uploads.buffered
	uploads.mu.Unlock()
	if buffered != 6 {
		t.Fatalf("buffered after discard = %d, want 6", buffered)
	}

	first.mu.Lock()
	uploads.discard(firstID, first)
	first.mu.Unlock()
	uploads.mu.Lock()
	buffered = uploads.buffered
	uploads.mu.Unlock()
	if buffered != 0 {
		t.Fatalf("buffered after all discard = %d", buffered)
	}
}
