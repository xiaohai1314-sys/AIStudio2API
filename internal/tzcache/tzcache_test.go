package tzcache

import (
	"testing"
	"time"
)

func TestLoadCachesLocation(t *testing.T) {
	first, err := Load("Asia/Shanghai")
	if err != nil {
		t.Fatalf("Load 合法时区失败: %v", err)
	}
	second, err := Load("Asia/Shanghai")
	if err != nil {
		t.Fatalf("再次 Load 失败: %v", err)
	}
	if first != second {
		t.Fatalf("缓存未生效，两次返回不同的 Location")
	}
	if _, offset := time.Date(2026, 1, 1, 0, 0, 0, 0, first).Zone(); offset != 8*3600 {
		t.Fatalf("时区偏移错误: %d", offset)
	}
}

func TestLoadCachesError(t *testing.T) {
	if _, err := Load("Not/AZone"); err == nil {
		t.Fatal("非法时区应当返回错误")
	}
	if _, err := Load("Not/AZone"); err == nil {
		t.Fatal("非法时区再次 Load 也应当返回错误")
	}
}
