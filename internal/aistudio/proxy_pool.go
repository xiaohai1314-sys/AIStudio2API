package aistudio

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	appconfig "github.com/Mag1cFall/AIStudio2API/internal/config"
)

// proxyFailureThreshold 为代理判定不可用前允许的连续网络错误次数
const proxyFailureThreshold = 3

// proxyScoreWindow 是代理健康评分的统计窗口
const proxyScoreWindow = 30 * time.Minute

// proxyQualityRanks 按出口质量排序，数值越大越优先
var proxyQualityRanks = map[string]int{"mobile": 4, "residential": 3, "isp": 2, "datacenter": 1}

// ProxyPool 把未固定代理的账户按绑定数分配到代理池，代理不可用时改绑到其他可用代理
type ProxyPool struct {
	mu      sync.Mutex
	pool    *AccountPool
	entries []*proxyEntry
	// scorePath 是健康评分的持久化文件路径，为空时不落盘
	scorePath string
	saveTimer *time.Timer
	closed    bool
}

// proxyEntry 表示代理池中的一个代理与其健康状态
type proxyEntry struct {
	proxy     string
	quality   string
	rank      int
	down      bool
	failures  int
	checkedAt time.Time
	lastError string
	// good 与 bad 是评分窗口内的成功与风控/网络失败次数，score 由二者算出
	good       int
	bad        int
	score      int
	scoreSince time.Time
}

// ProxyStatus 表示代理池中一个代理的可用性、健康评分与绑定账户数
type ProxyStatus struct {
	Proxy     string     `json:"proxy"`
	Quality   string     `json:"quality,omitempty"`
	Available bool       `json:"available"`
	Accounts  int        `json:"accounts"`
	Score     int        `json:"score"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// NewProxyPool 创建代理池，proxies 为空时账户使用系统代理或直连
func NewProxyPool(pool *AccountPool, proxies []string) *ProxyPool {
	entries := make([]*proxyEntry, 0, len(proxies))
	for _, proxy := range proxies {
		if proxy = strings.TrimSpace(proxy); proxy != "" {
			quality := appconfig.ProxyQuality(proxy)
			entries = append(entries, &proxyEntry{proxy: proxy, quality: quality, rank: proxyQualityRanks[quality], score: 100})
		}
	}
	return &ProxyPool{pool: pool, entries: entries}
}

// Assign 为未固定代理的账户绑定代理：保留指向可用代理的已有绑定，其余账户选绑定数最少的可用代理并写入运行状态，返回绑定变化的账户
func (p *ProxyPool) Assign() ([]string, error) {
	if p == nil {
		return nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.assignLocked()
}

func (p *ProxyPool) assignLocked() ([]string, error) {
	if len(p.entries) == 0 {
		return nil, nil
	}
	available := p.availableLocked()
	p.pool.mu.Lock()
	accounts := slices.Clone(p.pool.accounts)
	p.pool.mu.Unlock()
	counts := make(map[string]int, len(p.entries))
	bindings := make(map[*Account]string, len(accounts))
	pending := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		if strings.TrimSpace(account.Config.Proxy) != "" {
			continue
		}
		account.runtimeMu.Lock()
		bound := account.runtime.Proxy
		account.runtimeMu.Unlock()
		if current := account.boundProxy.Load(); bound == "" && current != nil {
			bound = *current
		}
		if slices.Contains(available, bound) {
			counts[bound]++
			bindings[account] = bound
			continue
		}
		pending = append(pending, account)
	}
	for _, account := range pending {
		chosen := p.chooseLocked(available, counts)
		counts[chosen]++
		bindings[account] = chosen
	}
	var changed []string
	var errs []error
	for _, account := range accounts {
		chosen, ok := bindings[account]
		if !ok {
			continue
		}
		previous := account.boundProxy.Load()
		_, err := p.pool.updateRuntime(account.ID, func(_ *Account, runtimeState *accountRuntimeState) (bool, func(*Account), error) {
			if runtimeState.Proxy == chosen {
				return false, nil, nil
			}
			runtimeState.Proxy = chosen
			return true, nil, nil
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		account.boundProxy.Store(&chosen)
		if previous != nil && *previous != chosen {
			changed = append(changed, account.ID)
		}
	}
	return changed, errors.Join(errs...)
}

// Next 返回下一个新账户将绑定的代理，代理池为空时返回空字符串
func (p *ProxyPool) Next() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) == 0 {
		return ""
	}
	available := p.availableLocked()
	counts := make(map[string]int, len(available))
	p.pool.mu.Lock()
	for _, account := range p.pool.accounts {
		if bound := account.boundProxy.Load(); bound != nil && strings.TrimSpace(account.Config.Proxy) == "" {
			counts[*bound]++
		}
	}
	p.pool.mu.Unlock()
	chosen := p.chooseLocked(available, counts)
	return chosen
}

// chooseLocked 在可用代理中选出出口质量最高、健康评分最高、绑定数最少的代理
func (p *ProxyPool) chooseLocked(available []string, counts map[string]int) string {
	chosen := available[0]
	chosenRank, chosenScore := p.rankOfLocked(chosen), p.scoreOfLocked(chosen)
	for _, proxy := range available[1:] {
		rank, score := p.rankOfLocked(proxy), p.scoreOfLocked(proxy)
		better := rank > chosenRank ||
			rank == chosenRank && (score > chosenScore || score == chosenScore && counts[proxy] < counts[chosen])
		if better {
			chosen, chosenRank, chosenScore = proxy, rank, score
		}
	}
	return chosen
}

// rankOfLocked 返回代理的出口质量等级，未标注时为 0
func (p *ProxyPool) rankOfLocked(proxy string) int {
	if entry := p.entryLocked(proxy); entry != nil {
		return entry.rank
	}
	return 0
}

// scoreOfLocked 返回代理的健康评分，未记录的代理按满分处理
func (p *ProxyPool) scoreOfLocked(proxy string) int {
	if entry := p.entryLocked(proxy); entry != nil {
		return entry.score
	}
	return 100
}

// recordLocked 记录一次成功或失败并重算评分，窗口过期后从零开始
func (entry *proxyEntry) recordLocked(good bool, now time.Time) {
	if now.Sub(entry.scoreSince) >= proxyScoreWindow {
		entry.good, entry.bad = 0, 0
		entry.scoreSince = now
	}
	if good {
		entry.good++
	} else {
		entry.bad++
	}
	entry.score = proxyScore(entry.good, entry.bad)
}

// proxyScore 返回 0 到 100 的健康评分，没有样本时为 100
func proxyScore(good, bad int) int {
	if good+bad == 0 {
		return 100
	}
	return int(math.Round(100 * float64(good) / float64(good+bad)))
}

// availableLocked 返回可用代理；全部不可用时返回全部代理
func (p *ProxyPool) availableLocked() []string {
	var available, all []string
	for _, entry := range p.entries {
		all = append(all, entry.proxy)
		if !entry.down {
			available = append(available, entry.proxy)
		}
	}
	if len(available) == 0 {
		return all
	}
	return available
}

// Binding 返回账户当前绑定的代理池代理，账户固定代理或未绑定时返回空字符串
func (p *ProxyPool) Binding(accountID string) string {
	if p == nil {
		return ""
	}
	p.pool.mu.Lock()
	account := p.pool.byID[strings.TrimSpace(accountID)]
	p.pool.mu.Unlock()
	if account == nil || strings.TrimSpace(account.Config.Proxy) != "" {
		return ""
	}
	if bound := account.boundProxy.Load(); bound != nil {
		return *bound
	}
	return ""
}

// Available 判断代理是否在当前参与分配的代理中
func (p *ProxyPool) Available(proxy string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Contains(p.availableLocked(), proxy)
}

// entryLocked 返回代理对应的条目，不在代理池中时返回 nil
func (p *ProxyPool) entryLocked(proxy string) *proxyEntry {
	for _, entry := range p.entries {
		if entry.proxy == proxy {
			return entry
		}
	}
	return nil
}

// ReportFailure 记录经 proxy 发出的请求的网络错误；连续达到阈值且存在其他可用代理时把该代理标为不可用并改绑，返回绑定变化的账户
func (p *ProxyPool) ReportFailure(proxy string, err error) ([]string, error) {
	if p == nil {
		return nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.entryLocked(proxy)
	if entry == nil {
		return nil, nil
	}
	entry.failures++
	entry.lastError = err.Error()
	entry.recordLocked(false, time.Now())
	p.scheduleSaveLocked()
	if entry.down || entry.failures < proxyFailureThreshold || !p.otherAvailableLocked(entry) {
		return nil, nil
	}
	entry.down = true
	return p.assignLocked()
}

// ReportRisk 记录经 proxy 发出的请求命中上游风控，计入健康评分
func (p *ProxyPool) ReportRisk(proxy string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.entryLocked(proxy); entry != nil {
		entry.recordLocked(false, time.Now())
		p.scheduleSaveLocked()
	}
}

// ReportSuccess 清零 proxy 的连续网络错误并记录一次成功
func (p *ProxyPool) ReportSuccess(proxy string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.entryLocked(proxy); entry != nil {
		entry.failures = 0
		entry.recordLocked(true, time.Now())
		p.scheduleSaveLocked()
	}
}

func (p *ProxyPool) otherAvailableLocked(target *proxyEntry) bool {
	for _, entry := range p.entries {
		if entry != target && !entry.down {
			return true
		}
	}
	return false
}

// Probe 用 check 逐个检测代理，失败的代理标为不可用，恢复的代理重新参与分配，返回绑定变化的账户
func (p *ProxyPool) Probe(ctx context.Context, check func(context.Context, string) error) ([]string, error) {
	if p == nil {
		return nil, nil
	}
	results := make([]error, len(p.entries))
	var wait sync.WaitGroup
	for index, entry := range p.entries {
		wait.Go(func() {
			results[index] = check(ctx, entry.proxy)
		})
	}
	wait.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	for index, entry := range p.entries {
		entry.checkedAt = now
		if results[index] != nil {
			entry.down = true
			entry.lastError = results[index].Error()
			continue
		}
		entry.down = false
		entry.failures = 0
		entry.lastError = ""
	}
	return p.assignLocked()
}

// Status 返回各代理的可用性与当前绑定账户数
func (p *ProxyPool) Status() []ProxyStatus {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	counts := make(map[string]int, len(p.entries))
	p.pool.mu.Lock()
	for _, account := range p.pool.accounts {
		if strings.TrimSpace(account.Config.Proxy) != "" {
			continue
		}
		if bound := account.boundProxy.Load(); bound != nil {
			counts[*bound]++
		}
	}
	p.pool.mu.Unlock()
	statuses := make([]ProxyStatus, 0, len(p.entries))
	for _, entry := range p.entries {
		status := ProxyStatus{Proxy: entry.proxy, Quality: entry.quality, Available: !entry.down, Accounts: counts[entry.proxy], Score: entry.score, Error: entry.lastError}
		if !entry.checkedAt.IsZero() {
			status.CheckedAt = timePointer(entry.checkedAt)
		}
		statuses = append(statuses, status)
	}
	return statuses
}
