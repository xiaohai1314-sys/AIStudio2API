package api

import (
	"context"
	"testing"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// TestRelayEventsForwardsAll 验证上游结束后转发全部事件并关闭下游
func TestRelayEventsForwardsAll(t *testing.T) {
	source := make(chan aistudio.Event, 3)
	for _, text := range []string{"a", "b", "c"} {
		source <- aistudio.Event{Kind: aistudio.EventText, Text: text}
	}
	close(source)
	out := make(chan aistudio.Event)
	go relayEvents(context.Background(), source, out)
	var got []string
	for event := range out {
		got = append(got, event.Text)
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("转发结果 = %v", got)
	}
}

// TestRelayEventsStopsReadingAtQueueLimit 验证下游不消费时队列达到上限即停止读取上游，把压力回传给上游
func TestRelayEventsStopsReadingAtQueueLimit(t *testing.T) {
	total := 4 * maxRelayQueue
	source := make(chan aistudio.Event, total)
	for range total {
		source <- aistudio.Event{Kind: aistudio.EventText, Text: "x"}
	}
	close(source)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		relayEvents(ctx, source, make(chan aistudio.Event))
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(source) > total-maxRelayQueue && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	remaining := len(source)
	cancel()
	<-done
	if remaining != total-maxRelayQueue {
		t.Fatalf("停止读取后上游剩余 %d 个事件, want %d", remaining, total-maxRelayQueue)
	}
}
