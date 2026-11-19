package app

import (
	"testing"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

func TestDispatchQueueLeaveWakesNextHead(t *testing.T) {
	queue := newDispatchQueue()
	first := queue.join(aistudio.AccountSelection{ModelID: "key"})
	second := queue.join(aistudio.AccountSelection{ModelID: "key"})
	_, firstOK := queue.turn(first)
	_, secondOK := queue.turn(second)
	if !firstOK || secondOK {
		t.Fatal("同条件请求应按到达先后取号")
	}
	queue.leave(first)
	select {
	case <-second.wake:
	default:
		t.Fatal("队首离开后应当唤醒下一个请求")
	}
	if _, ok := queue.turn(second); !ok {
		t.Fatal("队首离开后下一个请求应轮到取号")
	}
	queue.leave(second)
	if len(queue.waiters) != 0 {
		t.Fatalf("队列清空后应当删除键: %d", len(queue.waiters))
	}
}

func TestDispatchQueueStartingLetsNextTakeTurn(t *testing.T) {
	queue := newDispatchQueue()
	head := queue.join(aistudio.AccountSelection{ModelID: "key"})
	tail := queue.join(aistudio.AccountSelection{ModelID: "key"})
	queue.setStarting(head, true)
	select {
	case <-tail.wake:
	default:
		t.Fatal("队首等待 Worker 启动时应当唤醒下一个请求")
	}
	if _, ok := queue.turn(tail); !ok {
		t.Fatal("队首等待 Worker 启动时下一个请求应轮到取号")
	}
	queue.setStarting(head, false)
	if _, ok := queue.turn(tail); ok {
		t.Fatal("队首恢复取号后下一个请求应继续等待")
	}
}

func TestDispatchQueueLeaveIgnoresAbsentWaiter(t *testing.T) {
	queue := newDispatchQueue()
	waiter := queue.join(aistudio.AccountSelection{ModelID: "key"})
	queue.leave(waiter)
	queue.leave(waiter)
	queue.leave(&dispatchWaiter{key: "key"})
	if len(queue.waiters) != 0 {
		t.Fatalf("队列清空后应当删除键: %d", len(queue.waiters))
	}
}
