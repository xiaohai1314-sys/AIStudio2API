package camoufoxnative

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveViewReceiverPanicCancelsPush(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	received := make(chan error, 1)
	startLiveViewReceiver(cancel, received, func() error {
		panic("receiver failure")
	})

	view := NewLiveView()
	viewer := &liveViewer{wake: make(chan struct{}, 1), done: make(chan struct{})}
	pushed := make(chan error, 1)
	go func() { pushed <- view.push(ctx, nil, viewer) }()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("接收 goroutine panic 后没有取消推送 context")
	}
	select {
	case err := <-pushed:
		if err != nil {
			t.Fatalf("推送循环因接收 panic 取消后应正常退出，得到 %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("主推送循环未随接收 goroutine panic 退出")
	}
	select {
	case err := <-received:
		if err == nil || !strings.Contains(err.Error(), "登录窗口接收 panic: receiver failure") {
			t.Fatalf("接收 panic 应返回错误，得到 %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("接收 panic 未回送错误")
	}
}
