package app

import "testing"

func TestDispatchQueueLeaveWakesNextHead(t *testing.T) {
	queue := newDispatchQueue()
	first := queue.join("key")
	second := queue.join("key")
	<-first.wake
	queue.leave(first)
	select {
	case <-second.wake:
	default:
		t.Fatal("队首离开后应当唤醒下一个请求")
	}
	queue.leave(second)
	if len(queue.queues) != 0 {
		t.Fatalf("队列清空后应当删除键: %d", len(queue.queues))
	}
}

func TestDispatchQueueLeaveKeepsHeadWhenNotHead(t *testing.T) {
	queue := newDispatchQueue()
	head := queue.join("key")
	tail := queue.join("key")
	<-head.wake
	queue.leave(tail)
	select {
	case <-head.wake:
		t.Fatal("非队首离开不应唤醒队首")
	default:
	}
}

func TestDispatchQueueLeaveIgnoresAbsentWaiter(t *testing.T) {
	queue := newDispatchQueue()
	waiter := queue.join("key")
	queue.leave(waiter)
	queue.leave(waiter)
	queue.leave(&dispatchWaiter{key: "key"})
	if len(queue.queues) != 0 {
		t.Fatalf("队列清空后应当删除键: %d", len(queue.queues))
	}
}
