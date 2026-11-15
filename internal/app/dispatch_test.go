package app

import "testing"

func TestDispatchQueueLeaveWakesNextHead(t *testing.T) {
	queue := newDispatchQueue()
	first := queue.join("key")
	second := queue.join("key")
	if !queue.turn(first) || queue.turn(second) {
		t.Fatal("同条件请求应按到达先后取号")
	}
	queue.leave(first)
	select {
	case <-second.wake:
	default:
		t.Fatal("队首离开后应当唤醒下一个请求")
	}
	if !queue.turn(second) {
		t.Fatal("队首离开后下一个请求应轮到取号")
	}
	queue.leave(second)
	if len(queue.queues) != 0 {
		t.Fatalf("队列清空后应当删除键: %d", len(queue.queues))
	}
}

func TestDispatchQueueStartingLetsNextTakeTurn(t *testing.T) {
	queue := newDispatchQueue()
	head := queue.join("key")
	tail := queue.join("key")
	queue.setStarting(head, true)
	select {
	case <-tail.wake:
	default:
		t.Fatal("队首等待 Worker 启动时应当唤醒下一个请求")
	}
	if !queue.turn(tail) {
		t.Fatal("队首等待 Worker 启动时下一个请求应轮到取号")
	}
	queue.setStarting(head, false)
	if queue.turn(tail) {
		t.Fatal("队首恢复取号后下一个请求应继续等待")
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
