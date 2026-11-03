package aistudio

import (
	"errors"
	"strings"
	"time"
)

// RiskKind 是上游风控响应的一类
type RiskKind string

const (
	// RiskChallenge 表示命中人机验证或登录跳转
	RiskChallenge RiskKind = "challenge"
	// RiskForbidden 表示上游返回 403
	RiskForbidden RiskKind = "forbidden"
	// RiskRateLimited 表示上游返回 429
	RiskRateLimited RiskKind = "rate_limited"
)

const (
	// riskWindow 是风控计数的统计窗口
	riskWindow = 10 * time.Minute
	// RiskIsolationThreshold 是窗口内 403 次数达到后自动隔离账户的阈值
	RiskIsolationThreshold = 3
)

// AccountRisk 是账户在一个时间窗口内命中的风控响应
type AccountRisk struct {
	Challenge   int       `json:"challenge"`
	Forbidden   int       `json:"forbidden"`
	RateLimited int       `json:"rate_limited"`
	LastAt      time.Time `json:"last_at,omitempty"`
}

// IsZero 返回是否没有风控记录
func (risk AccountRisk) IsZero() bool {
	return risk.Challenge == 0 && risk.Forbidden == 0 && risk.RateLimited == 0
}

// accountRiskWindow 是带统计起点的风控计数
type accountRiskWindow struct {
	since time.Time
	AccountRisk
}

// ErrorStatus 返回错误携带的上游 HTTP 状态码，没有时为 0
func ErrorStatus(err error) int {
	var status interface{ HTTPStatus() int }
	if errors.As(err, &status) {
		return status.HTTPStatus()
	}
	return 0
}

// RecordRisk 记录账户命中一次风控响应并返回当前窗口内的计数
func (p *AccountPool) RecordRisk(accountID string, kind RiskKind) AccountRisk {
	p.mu.Lock()
	defer p.mu.Unlock()
	account := p.byID[strings.TrimSpace(accountID)]
	if account == nil {
		return AccountRisk{}
	}
	now := time.Now()
	window := p.risk[account.ID]
	if window == nil {
		window = &accountRiskWindow{}
		p.risk[account.ID] = window
	}
	if now.Sub(window.since) >= riskWindow {
		window.AccountRisk = AccountRisk{}
		window.since = now
	}
	switch kind {
	case RiskChallenge:
		window.Challenge++
	case RiskForbidden:
		window.Forbidden++
	case RiskRateLimited:
		window.RateLimited++
	}
	window.LastAt = now.UTC()
	return window.AccountRisk
}

// riskOfLocked 返回账户在当前窗口内的风控计数，调用方需持有 p.mu
func (p *AccountPool) riskOfLocked(account *Account, now time.Time) AccountRisk {
	window := p.risk[account.ID]
	if window == nil || now.Sub(window.since) >= riskWindow {
		return AccountRisk{}
	}
	return window.AccountRisk
}
