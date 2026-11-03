package aistudio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// proxyScoreSaveDelay 是评分变化后延迟落盘的时间，避免每个请求都写文件
const proxyScoreSaveDelay = 5 * time.Second

// proxyScoreFile 是代理健康评分的持久化格式，代理地址以哈希作为键，避免把凭据写入磁盘
type proxyScoreFile struct {
	Version   int                         `json:"version"`
	UpdatedAt time.Time                   `json:"updated_at"`
	Proxies   map[string]proxyScoreSample `json:"proxies"`
}

// proxyScoreSample 是一个代理在评分窗口内的样本
type proxyScoreSample struct {
	Good    int                `json:"good"`
	Bad     int                `json:"bad"`
	Since   time.Time          `json:"since"`
	History []proxyScoreBucket `json:"history,omitempty"`
}

// LoadScores 读取上次进程保存的评分样本，文件不存在时直接返回
func (p *ProxyPool) LoadScores(path string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scorePath = strings.TrimSpace(path)
	if p.scorePath == "" {
		return nil
	}
	data, err := os.ReadFile(p.scorePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file proxyScoreFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	now := time.Now()
	for _, entry := range p.entries {
		sample, ok := file.Proxies[proxyScoreKey(entry.proxy)]
		if !ok || sample.Good < 0 || sample.Bad < 0 {
			continue
		}
		if now.Sub(sample.Since) < proxyScoreWindow {
			entry.good, entry.bad, entry.scoreSince = sample.Good, sample.Bad, sample.Since
		}
		for _, bucket := range sample.History {
			if bucket.Hour.IsZero() || now.Sub(bucket.Hour) >= proxyHistoryWindow || bucket.Hour.After(now) {
				continue
			}
			entry.history[proxyHistoryIndex(bucket.Hour)] = bucket
		}
	}
	return nil
}

// Close 停止待执行的保存并立即落盘
func (p *ProxyPool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.saveTimer != nil {
		p.saveTimer.Stop()
		p.saveTimer = nil
	}
	return p.saveLocked()
}

// scheduleSaveLocked 在评分变化后延迟落盘，调用方需持有 p.mu
func (p *ProxyPool) scheduleSaveLocked() {
	if p.scorePath == "" || p.closed || p.saveTimer != nil {
		return
	}
	p.saveTimer = time.AfterFunc(proxyScoreSaveDelay, p.flushScores)
}

// flushScores 执行一次延迟保存；保存失败时等下一次评分变化再重试
func (p *ProxyPool) flushScores() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.saveTimer = nil
	if p.closed {
		return
	}
	_ = p.saveLocked()
}

// saveLocked 把当前窗口内的评分样本原子写入磁盘
func (p *ProxyPool) saveLocked() error {
	if p.scorePath == "" {
		return nil
	}
	now := time.Now()
	file := proxyScoreFile{Version: 2, UpdatedAt: now.UTC(), Proxies: make(map[string]proxyScoreSample, len(p.entries))}
	for _, entry := range p.entries {
		if entry.good+entry.bad == 0 && entry.historyTotalsEmpty(now) {
			continue
		}
		sample := proxyScoreSample{Good: entry.good, Bad: entry.bad, Since: entry.scoreSince.UTC()}
		cutoff := now.UTC().Add(-proxyHistoryWindow)
		for _, bucket := range entry.history {
			if bucket.Hour.IsZero() || bucket.Hour.Before(cutoff) || bucket.Hour.After(now.UTC()) {
				continue
			}
			sample.History = append(sample.History, bucket)
		}
		file.Proxies[proxyScoreKey(entry.proxy)] = sample
	}
	data, err := json.Marshal(file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.scorePath), 0o700); err != nil {
		return err
	}
	temporary := p.scorePath + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, p.scorePath)
}

// proxyScoreKey 返回代理地址的哈希，用作持久化键，避免把含凭据的地址写入磁盘
func proxyScoreKey(proxy string) string {
	sum := sha256.Sum256([]byte(proxy))
	return hex.EncodeToString(sum[:])
}
