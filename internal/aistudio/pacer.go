package aistudio

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// pacer 按账户限制上游请求间隔并叠加随机抖动，避免机器化的固定节奏
type pacer struct {
	mu       sync.Mutex
	next     map[string]time.Time
	interval time.Duration
	jitter   float64
	now      func() time.Time
}

// newPacer 创建限速器，每分钟请求数不为正时返回 nil 表示不限制
func newPacer(ratePerMinute int, jitterPercent int) *pacer {
	if ratePerMinute <= 0 {
		return nil
	}
	jitter := float64(jitterPercent) / 100
	if jitter < 0 {
		jitter = 0
	}
	if jitter > 0.9 {
		jitter = 0.9
	}
	return &pacer{
		next:     make(map[string]time.Time),
		interval: time.Minute / time.Duration(ratePerMinute),
		jitter:   jitter,
		now:      time.Now,
	}
}

// Wait 等到账户可以发送下一个请求，ctx 结束时返回 ctx.Err()
func (p *pacer) Wait(ctx context.Context, accountID string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	now := p.now()
	previous := p.next[accountID]
	next := previous
	if next.Before(now) {
		next = now
	}
	reserved := next.Add(p.spacing())
	p.next[accountID] = reserved
	p.mu.Unlock()
	delay := next.Sub(now)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		// 取消的请求不占用它预留的间隔；后面已有请求排队时保留更晚的预留
		p.mu.Lock()
		if p.next[accountID] == reserved {
			p.next[accountID] = previous
		}
		p.mu.Unlock()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// spacing 返回叠加抖动后的请求间隔，抖动不使间隔短于基准的十分之一
func (p *pacer) spacing() time.Duration {
	if p.jitter == 0 {
		return p.interval
	}
	spacing := time.Duration(float64(p.interval) * (1 + p.jitter*(2*rand.Float64()-1)))
	if minimum := p.interval / 10; spacing < minimum {
		return minimum
	}
	return spacing
}
