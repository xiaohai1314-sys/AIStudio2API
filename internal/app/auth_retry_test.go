package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// TestAuthRefreshKeepsAccountSource 验证没有 OAuth 续签材料的账户不可续签且认证状态原样保留，重新登录以新登录结果为准
func TestAuthRefreshKeepsAccountSource(t *testing.T) {
	directory := t.TempDir()
	storage := filepath.Join(directory, "storage-state.json")
	state := `{"cookies":[{"name":"SAPISID","value":"old","domain":".google.com","path":"/","secure":true,"sameSite":"Lax"}],"origins":[]}`
	if err := os.WriteFile(storage, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := aistudio.LoadStorageState(storage)
	if err != nil {
		t.Fatal(err)
	}
	account := &aistudio.Account{
		ID: "user@example.com", Directory: directory, StoragePath: storage,
		RuntimePath: filepath.Join(directory, "runtime-state.json"), StorageState: loaded,
		Config: aistudio.DefaultAccountConfig("user@example.com"),
	}
	pool := aistudio.NewAccountPool([]*aistudio.Account{account}, 1)
	lease, err := pool.AcquireAccount(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	var cookies []aistudio.StateCookie
	for _, name := range []string{"SAPISID", "__Secure-1PAPISID", "__Secure-3PAPISID"} {
		cookies = append(cookies, aistudio.StateCookie{Name: name, Value: "chrome", Domain: ".google.com", Path: "/", Expires: -1, Secure: true, SameSite: "Lax"})
	}
	refresher := &authRuntimeRefresher{
		pool: pool, requests: newRequestRegistry(t.Context()),
		importCurrent: func(_ context.Context, source aistudio.AuthSource, _ aistudio.ChromeOAuthMaterial, _ string) (aistudio.StorageState, error) {
			updated := aistudio.StorageState{Cookies: cookies}
			updated.SetAuthExtension(aistudio.AuthExtension{
				Source: source, OAuth: &aistudio.ChromeOAuthMaterial{GaiaID: "gaia", RefreshToken: "token"},
			})
			return updated, nil
		},
		reset: func(string) error { return nil },
	}
	ctx := aistudio.ContextWithAccountLease(t.Context(), lease)
	if refresher.Available(ctx) {
		t.Fatal("没有 OAuth 续签材料的账户被判定为可续签")
	}
	if err := refresher.Refresh(ctx, lease.AuthGeneration()); err == nil {
		t.Fatal("没有 OAuth 续签材料的账户续签成功")
	}
	if stored, err := os.ReadFile(storage); err != nil || string(stored) != state {
		t.Fatalf("续签失败后认证状态被改写: %s err=%v", stored, err)
	}

	chrome := aistudio.StorageState{Cookies: cookies}
	chrome.SetAuthExtension(aistudio.AuthExtension{
		Source: aistudio.AuthSource{Browser: "chrome", Email: account.ID},
		OAuth:  &aistudio.ChromeOAuthMaterial{GaiaID: "gaia", RefreshToken: "token"},
	})
	if err := lease.SaveStorageState(chrome); err != nil {
		t.Fatal(err)
	}
	login := aistudio.StorageState{Cookies: cookies}
	login.SetAuthExtension(aistudio.AuthExtension{Source: aistudio.AuthSource{Browser: "camoufox", Email: account.ID}})
	if err := replaceAccountState(lease, login, ""); err != nil {
		t.Fatal(err)
	}
	stored, err := aistudio.LoadStorageState(storage)
	if err != nil {
		t.Fatal(err)
	}
	if extension, _, err := stored.AuthExtension(); err != nil || extension.OAuth != nil || extension.Source.Browser != "camoufox" {
		t.Fatalf("重新登录后的认证扩展 %+v err=%v", extension, err)
	}
}

// TestEnsureBoundRejectsRotationAfterAuthGenerationAdvances 验证认证代际推进时，旧 DBSC 旋转 Cookie 不会被合并或推给 Worker
func TestEnsureBoundRejectsRotationAfterAuthGenerationAdvances(t *testing.T) {
	const accountID = "user@example.com"
	const refreshURL = "https://accounts.google.com/RotateBoundSession"
	directory := t.TempDir()
	storage := filepath.Join(directory, "storage-state.json")
	newState := func(value string, expires time.Time) aistudio.StorageState {
		state := aistudio.StorageState{Cookies: []aistudio.StateCookie{{
			Name: "SIDCC", Value: value, Domain: ".google.com", Path: "/", Expires: float64(expires.Unix()), Secure: true,
		}}}
		state.SetAuthExtension(aistudio.AuthExtension{
			Source: aistudio.AuthSource{Browser: "chrome", Email: accountID},
			Bound:  &aistudio.BoundSession{ID: "same-session", RefreshURL: refreshURL, Credentials: []string{"SIDCC"}},
		})
		return state
	}
	initial := newState("old", time.Now().Add(time.Minute))
	if err := aistudio.WriteStorageState(storage, initial); err != nil {
		t.Fatal(err)
	}
	account := &aistudio.Account{
		ID: accountID, Directory: directory, StoragePath: storage, StorageState: initial,
		Config: aistudio.DefaultAccountConfig(accountID),
	}
	pool := aistudio.NewAccountPool([]*aistudio.Account{account}, 1)
	lease, err := pool.AcquireAccount(t.Context(), accountID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	workerMerged := false
	refresher := &authRuntimeRefresher{
		pool: pool,
		rotate: func(_ context.Context, state aistudio.StorageState, _ aistudio.AuthExtension, _ string) ([]string, error) {
			if len(state.Cookies) != 1 || state.Cookies[0].Value != "old" {
				t.Fatalf("旋转应使用快照中的旧状态，得到 %+v", state.Cookies)
			}
			if err := lease.SaveStorageState(newState("new", time.Now().Add(time.Hour))); err != nil {
				t.Errorf("并发保存新认证状态: %v", err)
			}
			return []string{"SIDCC=stale-rotated; Domain=.google.com; Path=/; Secure"}, nil
		},
		mergeWorker: func(context.Context, string, []string, string) error {
			workerMerged = true
			return nil
		},
	}
	ctx := aistudio.ContextWithAccountLease(t.Context(), lease)
	if err := refresher.ensureBound(ctx); err != nil {
		t.Fatalf("并发续签后应丢弃旧旋转结果而不报错: %v", err)
	}
	stored, err := aistudio.LoadStorageState(storage)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Cookies) != 1 || stored.Cookies[0].Value != "new" {
		t.Fatalf("旧 DBSC Cookie 覆盖了并发续签状态: %+v", stored.Cookies)
	}
	if workerMerged {
		t.Fatal("旧 DBSC Cookie 不应推送到 Worker 页面")
	}
}
