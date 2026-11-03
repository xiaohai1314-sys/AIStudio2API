package aistudio

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestPacerDisabled 验证每分钟请求数不为正时限速器不生效
func TestPacerDisabled(t *testing.T) {
	if newPacer(0, 25) != nil {
		t.Fatal("请求数上限为 0 时应返回 nil")
	}
	var disabled *pacer
	if err := disabled.Wait(context.Background(), "account"); err != nil {
		t.Fatalf("未启用时限速不应报错: %v", err)
	}
}

// TestPacerSpacingBounds 验证抖动后的间隔保持在基准的十分之一到两倍之间
func TestPacerSpacingBounds(t *testing.T) {
	pacer := newPacer(60, 50)
	for range 200 {
		spacing := pacer.spacing()
		if spacing < pacer.interval/10 || spacing > pacer.interval*2 {
			t.Fatalf("间隔 %s 超出范围，基准 %s", spacing, pacer.interval)
		}
	}
}

// TestPacerWaitsBetweenRequests 验证同一账户的连续请求按间隔排队，不同账户互不影响
func TestPacerWaitsBetweenRequests(t *testing.T) {
	pacer := newPacer(60, 0)
	if err := pacer.Wait(context.Background(), "first"); err != nil {
		t.Fatalf("首次请求不应等待: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := pacer.Wait(ctx, "first"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("同一账户的第二次请求应等待到超时，实际 %v", err)
	}
	if err := pacer.Wait(context.Background(), "second"); err != nil {
		t.Fatalf("其他账户的首次请求不应等待: %v", err)
	}
}

// TestPacerCanceledContext 验证等待可被请求取消打断
func TestPacerCanceledContext(t *testing.T) {
	pacer := newPacer(1, 0)
	if err := pacer.Wait(context.Background(), "account"); err != nil {
		t.Fatalf("首次请求不应等待: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pacer.Wait(ctx, "account"); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应返回 context.Canceled，实际 %v", err)
	}
}
