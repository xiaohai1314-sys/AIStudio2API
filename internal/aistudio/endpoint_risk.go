package aistudio

import (
	"sort"
	"strings"
	"time"
)

// endpointRiskWindow 是按模型与通道统计风控响应的窗口
const endpointRiskWindow = 10 * time.Minute

// EndpointOutcome 是一次上游尝试的结果类别
type EndpointOutcome string

const (
	// EndpointSuccess 表示收到首个事件
	EndpointSuccess EndpointOutcome = "success"
	// EndpointChallenge 表示命中人机验证或登录跳转
	EndpointChallenge EndpointOutcome = "challenge"
	// EndpointForbidden 表示上游返回 403
	EndpointForbidden EndpointOutcome = "forbidden"
	// EndpointRateLimited 表示上游返回 429
	EndpointRateLimited EndpointOutcome = "rate_limited"
	// EndpointFailed 表示其他失败
	EndpointFailed EndpointOutcome = "failed"
)

// EndpointRisk 是某个模型与通道在当前窗口内的请求与风控计数
type EndpointRisk struct {
	Model       string    `json:"model"`
	Channel     Channel   `json:"channel"`
	Requests    int       `json:"requests"`
	Succeeded   int       `json:"succeeded"`
	Failed      int       `json:"failed"`
	Challenge   int       `json:"challenge"`
	Forbidden   int       `json:"forbidden"`
	RateLimited int       `json:"rate_limited"`
	LastAt      time.Time `json:"last_at,omitempty"`
}

// endpointRiskEntry 是带统计起点的端点计数
type endpointRiskEntry struct {
	since time.Time
	EndpointRisk
}

// RecordEndpoint 记录一次上游尝试的结果，按通道与模型汇总到当前窗口
func (p *AccountPool) RecordEndpoint(model string, channel Channel, outcome EndpointOutcome) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	key := string(channel) + ":" + model
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	entry := p.endpointRisk[key]
	if entry == nil {
		entry = &endpointRiskEntry{}
		p.endpointRisk[key] = entry
	}
	if now.Sub(entry.since) >= endpointRiskWindow {
		entry.EndpointRisk = EndpointRisk{Model: model, Channel: channel}
		entry.since = now
	}
	entry.Requests++
	switch outcome {
	case EndpointSuccess:
		entry.Succeeded++
	case EndpointChallenge:
		entry.Challenge++
		entry.Failed++
	case EndpointForbidden:
		entry.Forbidden++
		entry.Failed++
	case EndpointRateLimited:
		entry.RateLimited++
		entry.Failed++
	default:
		entry.Failed++
	}
	entry.LastAt = now.UTC()
}

// EndpointRisks 返回当前窗口内的端点计数，风控命中多的排在前面
func (p *AccountPool) EndpointRisks() []EndpointRisk {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	risks := make([]EndpointRisk, 0, len(p.endpointRisk))
	for key, entry := range p.endpointRisk {
		if now.Sub(entry.since) >= endpointRiskWindow {
			delete(p.endpointRisk, key)
			continue
		}
		risks = append(risks, entry.EndpointRisk)
	}
	sort.Slice(risks, func(i, j int) bool {
		left, right := endpointRiskHits(risks[i]), endpointRiskHits(risks[j])
		if left != right {
			return left > right
		}
		if risks[i].Requests != risks[j].Requests {
			return risks[i].Requests > risks[j].Requests
		}
		return risks[i].Model < risks[j].Model
	})
	return risks
}

// endpointRiskHits 返回端点命中风控的总次数
func endpointRiskHits(risk EndpointRisk) int {
	return risk.Challenge + risk.Forbidden + risk.RateLimited
}
