package app

import (
	"context"
	"errors"
	"net/http"
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
	manager := newAccountWorkerManager(pool, []*aistudio.Account{account}, newRequestRegistry(), "", "", time.Minute, 0, 2, 1)
	defer func() { _ = manager.Close() }()
	service := &trackedService{pool: pool, workers: manager}
	service.SetRequestPolicy(nil, 3)

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

// TestFirstRejectionCoolsModelOnly 是 F2 回归测试（用户 2026-11-17 决定：首次冷却，换模型不冷却）：
// 上游 16a90c5 把首次 403/code 7 改成只记录不冷却、且无其他账户时同账户重试一次；
// fork 恢复首次即冷却：第一次被拒就在该模型上按 planLimitBackoff 冷却（首级 1 分钟），
// 且冷却只作用于被拒的模型——同一账户换另一个模型不受影响，可以直接拿租约。
func TestFirstRejectionCoolsModelOnly(t *testing.T) {
	account := &aistudio.Account{
		ID:     "rejected@example.com",
		Config: aistudio.AccountConfig{Enabled: true},
		State:  aistudio.AccountReady,
		Models: []aistudio.Model{{ID: "gemini-4-argon"}, {ID: "gemini-4-carbon"}},
	}
	pool := aistudio.NewAccountPool([]*aistudio.Account{account}, 2)
	// 构造一次上游 403、协议错误码 7 的拒绝（消息不触发官网拒绝页跳转）。
	rejectionErr := &aistudio.RPCError{StatusCode: http.StatusForbidden, Code: 7, Message: "Forbidden"}
	cooldown, ok := aistudio.AccountRejectionCooldown(rejectionErr)
	if !ok {
		t.Fatalf("AccountRejectionCooldown 没有识别 403/code 7 拒绝")
	}
	now := time.Now()
	until, changed, err := pool.MarkQuotaCooldownIfGeneration(account.ID, "gemini-4-argon", 0, now, cooldown)
	if err != nil {
		t.Fatalf("标记权限拒绝冷却失败: %v", err)
	}
	if !changed {
		t.Fatalf("首次被拒没有改变冷却状态")
	}
	if !until.After(now) || until.After(now.Add(2*time.Minute)) {
		t.Fatalf("首次被拒的冷却恢复时刻 = %v，想要约 1 分钟后", until)
	}
	if !pool.AccountCoolingDown(account.ID, "gemini-4-argon") {
		t.Fatalf("首次被拒后账户在 gemini-4-argon 上应处于冷却")
	}
	if pool.AccountCoolingDown(account.ID, "gemini-4-carbon") {
		t.Fatalf("账户在 gemini-4-carbon 上不应被 gemini-4-argon 的冷却影响")
	}
	manager := newAccountWorkerManager(pool, []*aistudio.Account{account}, newRequestRegistry(), "", "", time.Minute, 0, 2, 1)
	defer func() { _ = manager.Close() }()
	service := &trackedService{pool: pool, workers: manager}
	service.SetRequestPolicy(nil, 3)

	failures := 0
	var cooled time.Duration
	lease, err := service.acquireWarmLease(context.Background(), aistudio.AccountSelection{ModelID: "gemini-4-argon"}, &failures, &cooled)
	if lease != nil {
		_ = lease.Release()
		t.Fatalf("acquireWarmLease 返回了租约 %+v，首次被拒冷却的模型不应拿租约", lease)
	}
	var cooling *aistudio.AllCoolingError
	if !errors.As(err, &cooling) {
		t.Fatalf("acquireWarmLease 错误 = %v，想要 AllCoolingError", err)
	}

	// 换模型不冷却：同一账户可以直接为另一个模型拿租约。
	lease, _, err = pool.TryAcquireFor(context.Background(), aistudio.AccountSelection{ModelID: "gemini-4-carbon"})
	if err != nil {
		t.Fatalf("换模型拿租约失败: %v", err)
	}
	if lease == nil {
		t.Fatalf("换模型应能拿租约，得到 nil")
	}
	defer func() { _ = lease.Release() }()
}
