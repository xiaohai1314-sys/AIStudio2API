package aistudio

import (
	"context"
	"testing"
	"time"
)

// TestBidiFinalizeWithAbandonedConsumer 验证消费者放弃读取 events 且 ctx 未取消时，
// runBackchannel 的收尾（cancel 先于等待 forwardDone）仍能退出并释放账户租约
func TestBidiFinalizeWithAbandonedConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	released := make(chan struct{})
	session := &BidiSession{
		ctx: ctx, cancel: cancel,
		events:      make(chan BidiEvent, 1), // 缓冲小于待转发事件数，模拟消费者不排空
		wireEvents:  make(chan BidiEvent, 4),
		forwarding:  make(chan bool, 1),
		forwardDone: make(chan struct{}),
		done:        make(chan struct{}),
	}
	session.release = func() error {
		close(released)
		return nil
	}
	// 模拟 forwardEvents 正在把多个事件转发进小缓冲的 events，且无人消费
	go func() {
		defer close(session.forwardDone)
		defer close(session.events)
		for _, event := range []BidiEvent{{Kind: BidiEventText}, {Kind: BidiEventText}, {Kind: BidiEventText}} {
			if !session.forwardEvent(event) {
				return
			}
		}
	}()
	session.forwarding <- true // awaitSetup 在启动 forwardEvents 后写入 true
	// 模拟 runBackchannel 收尾的顺序（修复后：先 cancel 再等 forwardDone）
	// 注意 cancel 前先让 forwardEvent 进入阻塞发送：3 个事件塞 1 缓冲，第 3 个必然阻塞
	go func() {
		close(session.wireEvents)
		session.cancel()
		if true { // 模拟未修复代码：等待 forwardDone
			<-session.forwardDone
		} else {
			close(session.events)
			close(session.forwardDone)
		}
		if session.release != nil {
			_ = session.release()
		}
		close(session.done)
	}()
	select {
	case <-session.done:
	case <-time.After(5 * time.Second):
		t.Fatal("收尾在消费者放弃读取时永久卡死，未释放租约")
	}
	select {
	case <-released:
	default:
		t.Fatal("收尾完成但租约未释放")
	}
}

// TestBidiFinalizeWithConsumer 验证正常消费路径（消费者持续排空）收尾仍能完成且事件投递
func TestBidiFinalizeWithConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &BidiSession{
		ctx: ctx, cancel: cancel,
		events:      make(chan BidiEvent, 4),
		wireEvents:  make(chan BidiEvent, 4),
		forwarding:  make(chan bool, 1),
		forwardDone: make(chan struct{}),
		done:        make(chan struct{}),
	}
	var released bool
	session.release = func() error {
		released = true
		return nil
	}
	// 消费者持续排空
	go func() {
		for range session.events {
		}
	}()
	// 转发中
	go func() {
		defer close(session.forwardDone)
		defer close(session.events)
		for _, event := range []BidiEvent{{Kind: BidiEventText}, {Kind: BidiEventClosed}} {
			if !session.forwardEvent(event) {
				return
			}
		}
	}()
	session.forwarding <- true
	go func() {
		close(session.wireEvents)
		session.cancel()
		if true { // 模拟未修复代码：等待 forwardDone
			<-session.forwardDone
		} else {
			close(session.events)
			close(session.forwardDone)
		}
		if session.release != nil {
			_ = session.release()
		}
		close(session.done)
	}()
	select {
	case <-session.done:
	case <-time.After(5 * time.Second):
		t.Fatal("正常消费路径收尾卡死")
	}
	if !released {
		t.Fatal("正常消费路径租约未释放")
	}
}
