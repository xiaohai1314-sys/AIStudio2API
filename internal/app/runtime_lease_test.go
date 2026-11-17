package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// TestAcquireWarmLeaseAllCoolingReturnsAllCoolingError 是 PR #16 回归测试：
// 全部候选账户都在套餐限额冷却中、最早恢复晚于排队上限时，acquireWarmLease
// 必须直接返回 AllCoolingError，不拿租约、不把请求发往冷却中的账户。
// 上游 16a90c5 曾把冷却回退参数带回来（放宽到权限拒绝冷却）；fork 不接受
// 该回退，本测试守住这条线。
func TestAcquireWarmLeaseAllCoolingReturnsAllCoolingError(t *testing.T) {
	account := &aistudio.Account{
		ID:     "cooled@example.com",
		Config: aistudio.AccountConfig{Enabled: true},
		State:  aistudio.AccountReady,
		Models: []aistudio.Model{{ID: "gemini-4-argon"}},
	}
	pool := aistudio.NewAccountPool([]*aistudio.Account{account}, 2)
	if _, _, err := pool.MarkQuotaCooldownIfGeneration(account.ID, "", 0, time.Now(), aistudio.QuotaCooldown{
		Until:  time.Now().Add(2 * time.Hour),
		Reason: aistudio.PlanLimitKind,
	}); err != nil {
		t.Fatalf("标记套餐限额冷却失败: %v", err)
	}
	manager := newAccountWorkerManager(pool, []*aistudio.Account{account}, newRequestRegistry(context.Background()), "", "", time.Minute, 0, 2, 1)
	defer func() { _ = manager.Close() }()
	service := &trackedService{pool: pool, workers: manager, maxAccountSwitches: 3}

	failures := 0
	var cooled time.Duration
	lease, err := service.acquireWarmLease(context.Background(), aistudio.AccountSelection{ModelID: "gemini-4-argon"}, &failures, &cooled)
	if lease != nil {
		t.Fatalf("acquireWarmLease 返回了租约 %+v，全员冷却时不应拿租约", lease)
	}
	var cooling *aistudio.AllCoolingError
	if !errors.As(err, &cooling) {
		t.Fatalf("acquireWarmLease 错误 = %v，想要 AllCoolingError", err)
	}
	if cooling.ModelID != "gemini-4-argon" {
		t.Fatalf("AllCoolingError.ModelID = %q，想要 gemini-4-argon", cooling.ModelID)
	}
	if time.Until(cooling.Until) < time.Hour {
		t.Fatalf("AllCoolingError.Until = %v，恢复时间应约 2 小时后", cooling.Until)
	}
}
