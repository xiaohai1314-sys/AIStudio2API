package aistudio

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMergeBoundSessionCookiesGenerationGuard 验证 DBSC 旋转结果只在绑定会话未被换掉时才合并：
// 旋转期间账户续签换代后，旧会话 Cookie 必须按代际变化作废，而不是覆盖新会话状态
func TestMergeBoundSessionCookiesGenerationGuard(t *testing.T) {
	const refreshURL = "https://accounts.google.com/RotateBoundSession"
	account := &Account{
		ID:          "user@example.com",
		State:       AccountReady,
		Config:      AccountConfig{Enabled: true, Label: "user@example.com"},
		StoragePath: filepath.Join(t.TempDir(), "storage.json"),
	}
	state := StorageState{}
	state.SetAuthExtension(AuthExtension{
		Source: AuthSource{Browser: "chrome", Email: account.ID},
		Bound:  &BoundSession{ID: "session-new", RefreshURL: refreshURL},
	})
	if err := WriteStorageState(account.StoragePath, state); err != nil {
		t.Fatal(err)
	}
	pool := NewAccountPool([]*Account{account}, 1)
	lease, err := pool.AcquireFor(context.Background(), AccountSelection{AccountID: account.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	headers := []string{"__Host-GAPS=fresh; Path=/; Secure; HttpOnly"}
	stale := BoundSession{ID: "session-old", RefreshURL: refreshURL}
	if err := lease.MergeBoundSessionCookies(headers, stale, time.Now()); !errors.Is(err, ErrStorageGenerationChanged) {
		t.Fatalf("绑定会话已变化时应按代际变化作废，得到 %v", err)
	}
	current := BoundSession{ID: "session-new", RefreshURL: refreshURL}
	if err := lease.MergeBoundSessionCookies(headers, current, time.Now()); err != nil {
		t.Fatalf("绑定会话一致时应合并成功: %v", err)
	}
	reloaded, err := LoadStorageState(account.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	header, err := reloaded.CookieHeader(refreshURL, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(header, "__Host-GAPS=fresh") {
		t.Fatalf("合并后的 Cookie 应可见，得到 %q", header)
	}
}
