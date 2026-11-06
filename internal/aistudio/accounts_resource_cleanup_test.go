package aistudio

import (
	"context"
	"errors"
	"testing"
)

// TestUnbindResourceIfPresentContextMissingIsSuccess 验证清理路径重复解绑已不存在资源时保持幂等。
func TestUnbindResourceIfPresentContextMissingIsSuccess(t *testing.T) {
	pool := NewAccountPool(nil, 1)
	if err := pool.unbindResourceIfPresentContext(context.Background(), "missing-resource"); err != nil {
		t.Fatalf("缺失资源的幂等解绑不应报错: %v", err)
	}
	if err := pool.unbindResourceContext(context.Background(), "missing-resource"); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("底层解绑应保留 ErrResourceNotFound 语义，得到 %v", err)
	}
}
