package aistudio

import (
	"context"
	"testing"
	"time"
)

// newFinalizeTestSession 构造只含收尾所需字段的会话，转发与释放逻辑均为真实实现
func newFinalizeTestSession(eventsCap int, released chan struct{}) *BidiSession {
	ctx, cancel := context.WithCancel(context.Background())
	session := &BidiSession{
		ctx: ctx, cancel: cancel,
		events:      make(chan BidiEvent, eventsCap),
		wireEvents:  make(chan BidiEvent, 4),
		forwarding:  make(chan bool, 1),
		forwardDone: make(chan struct{}),
		done:        make(chan struct{}),
	}
	session.release = func() error {
		close(released)
		return nil
	}
	return session
}

// TestBidiFinalizeWithAbandonedConsumer 验证消费者放弃读取 events 且 ctx 未取消时，
// 真实收尾逻辑仍能退出并释放账户租约（修复前 <-forwardDone 先于 cancel，会永久卡死）
func TestBidiFinalizeWithAbandonedConsumer(t *testing.T) {
	released := make(chan struct{})
	session := newFinalizeTestSession(1, released) // 缓冲 1 小于待转发事件数，模拟消费者不排空
	go session.forwardEvents(nil)
	session.forwarding <- true // awaitSetup 在启动 forwardEvents 后写入 true
	session.wireEvents <- BidiEvent{Kind: BidiEventText}
	session.wireEvents <- BidiEvent{Kind: BidiEventText}
	session.wireEvents <- BidiEvent{Kind: BidiEventText}

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		session.finalize(nil, false)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("收尾在消费者放弃读取时永久卡死")
	}
	select {
	case <-released:
	default:
		t.Fatal("收尾完成但租约未释放")
	}
}

// TestBidiFinalizeWithConsumer 验证消费者持续排空时，真实收尾逻辑完成、事件全部投递且租约释放
func TestBidiFinalizeWithConsumer(t *testing.T) {
	released := make(chan struct{})
	session := newFinalizeTestSession(4, released)
	delivered := make(chan []BidiEvent, 1)
	go func() {
		var events []BidiEvent
		for event := range session.events {
			events = append(events, event)
		}
		delivered <- events
	}()
	go session.forwardEvents(nil)
	session.forwarding <- true
	session.wireEvents <- BidiEvent{Kind: BidiEventText}
	session.wireEvents <- BidiEvent{Kind: BidiEventText}

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		session.finalize(nil, false)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("正常消费路径收尾卡死")
	}
	select {
	case <-released:
	default:
		t.Fatal("正常消费路径租约未释放")
	}
	select {
	case events := <-delivered:
		// 两条文本事件 + 收尾投递的 closed 事件
		if len(events) != 3 || events[len(events)-1].Kind != BidiEventClosed {
			t.Fatalf("投递事件 = %v，want 2 条文本 + closed", events)
		}
	case <-time.After(time.Second):
		t.Fatal("消费者未收到全部事件")
	}
}

// TestBidiFinalizeWithFullWireEvents 验证消费者放弃且 wireEvents 已满时，终止事件投递有超时兜底，收尾不卡死
func TestBidiFinalizeWithFullWireEvents(t *testing.T) {
	released := make(chan struct{})
	session := newFinalizeTestSession(1, released)
	// 不启动 forwardEvents：wireEvents 塞满后终止事件无处可去
	for range cap(session.wireEvents) {
		session.wireEvents <- BidiEvent{Kind: BidiEventText}
	}
	session.forwarding <- false // 未进入转发阶段

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		session.finalize(nil, false)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("wireEvents 满时收尾卡死")
	}
	select {
	case <-released:
	default:
		t.Fatal("租约未释放")
	}
}
