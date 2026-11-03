// Package tzcache 缓存 time.LoadLocation 的结果，避免重复读取并解析 tzdata
package tzcache

import (
	"sync"
	"sync/atomic"
	"time"
)

// maxEntries 限制缓存条目数，超过后不再写入，仍按 time.LoadLocation 的结果返回
const maxEntries = 1024

type entry struct {
	location *time.Location
	err      error
}

var (
	locations sync.Map
	entries   atomic.Int64
)

// Load 与 time.LoadLocation 行为一致，按名称缓存解析结果
func Load(name string) (*time.Location, error) {
	if cached, ok := locations.Load(name); ok {
		value := cached.(entry)
		return value.location, value.err
	}
	location, err := time.LoadLocation(name)
	if entries.Load() < maxEntries {
		if _, loaded := locations.LoadOrStore(name, entry{location: location, err: err}); !loaded {
			entries.Add(1)
		}
	}
	return location, err
}
