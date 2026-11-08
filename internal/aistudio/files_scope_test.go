package aistudio

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scopedFileAccount(id, fileID string, created time.Time) *Account {
	return &Account{
		ID:     id,
		State:  AccountReady,
		Config: AccountConfig{Enabled: true, Label: id},
		runtime: accountRuntimeState{Resources: map[string]ResourceBinding{
			fileID: {Kind: "drive-file", Name: fileID + ".txt", MIME: "text/plain", Size: 8, Purpose: "user", CreatedAt: created},
		}},
	}
}

func fileIDs(files []FileMetadata) []string {
	ids := make([]string, 0, len(files))
	for _, file := range files {
		ids = append(ids, file.ID)
	}
	return ids
}

// TestFileAPIKeyScope 验证列表、详情和按资源选号都限制在当前 key 的账户内。
// 跨 key 的文件与不存在的文件同样是 ErrResourceNotFound，错误里不出现对方账户。未绑定的 key 仍可看到全部账户。
func TestFileAPIKeyScope(t *testing.T) {
	accountA := scopedFileAccount("a@example.com", "file-a", time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	accountB := scopedFileAccount("b@example.com", "file-b", time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC))
	pool := NewAccountPool([]*Account{accountA, accountB}, 2)
	pool.SetAPIKeyAccounts(map[string][]string{
		"key-a":    {"a@example.com"},
		"key-b":    {"b@example.com"},
		"key-none": {},
	})

	ctxA := ContextWithAPIKey(context.Background(), "key-a")
	files, err := pool.ListFiles(ctxA)
	if err != nil || strings.Join(fileIDs(files), ",") != "file-a" {
		t.Fatalf("key-a 列表 = %v, %v", fileIDs(files), err)
	}
	metadata, err := pool.FileMetadata(ctxA, "file-a")
	if err != nil || metadata.ID != "file-a" || metadata.Name != "file-a.txt" {
		t.Fatalf("key-a 读取自己的文件 = %+v, %v", metadata, err)
	}
	_, err = pool.FileMetadata(ctxA, "file-b")
	if !errors.Is(err, ErrResourceNotFound) || strings.Contains(err.Error(), "b@example.com") {
		t.Fatalf("跨 key 详情应表现为不存在，得到 %v", err)
	}
	if _, err := pool.AcquireFor(ctxA, AccountSelection{ResourceID: "file-b"}); !errors.Is(err, ErrResourceNotFound) || errors.Is(err, ErrNoEligibleAccount) {
		t.Fatalf("跨 key 下载选号应表现为不存在，得到 %v", err)
	}
	lease, err := pool.AcquireFor(ctxA, AccountSelection{ResourceID: "file-a"})
	if err != nil || lease.Account().ID != accountA.ID {
		t.Fatalf("key-a 应能选中自己的文件账户: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}

	ctxB := ContextWithAPIKey(context.Background(), "key-b")
	files, err = pool.ListFiles(ctxB)
	if err != nil || strings.Join(fileIDs(files), ",") != "file-b" {
		t.Fatalf("key-b 列表 = %v, %v", fileIDs(files), err)
	}

	files, err = pool.ListFiles(context.Background())
	if err != nil || len(files) != 2 {
		t.Fatalf("未带 key 的调用应看到全部文件: %v %v", fileIDs(files), err)
	}
	files, err = pool.ListFiles(ContextWithAPIKey(context.Background(), "key-other"))
	if err != nil || len(files) != 2 {
		t.Fatalf("未绑定的 key 应看到全部文件: %v %v", fileIDs(files), err)
	}
	files, err = pool.ListFiles(ContextWithAPIKey(context.Background(), "key-none"))
	if err != nil || len(files) != 0 {
		t.Fatalf("没有账户的 key 不应看到文件: %v %v", fileIDs(files), err)
	}
	if _, err := pool.FileMetadata(ContextWithAPIKey(context.Background(), "key-none"), "file-a"); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("空绑定 key 的详情 = %v", err)
	}
}

// TestDeletedAccountFilesDisappear 验证账户目录删除后，列表和详情都不再返回该账户的文件。
func TestDeletedAccountFilesDisappear(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "gone@example.com")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"account.json", "storage-state.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := accountRuntimeState{Resources: map[string]ResourceBinding{
		"file-gone": {Kind: "drive-file", Name: "gone.txt", MIME: "text/plain", Size: 4, Purpose: "user", CreatedAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
	}}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, runtimeStateName), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	deleted := &Account{
		ID: directoryBase(directory), Directory: directory,
		ConfigPath: filepath.Join(directory, accountConfigName), StoragePath: filepath.Join(directory, storageStateName),
		RuntimePath: filepath.Join(directory, runtimeStateName), State: AccountReady, Config: AccountConfig{Enabled: true, Label: "gone@example.com"},
	}
	kept := scopedFileAccount("keep@example.com", "file-keep", time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC))
	pool := NewAccountPool([]*Account{deleted, kept}, 1)

	files, err := pool.ListFiles(context.Background())
	if err != nil || !containsFile(files, "file-gone") || !containsFile(files, "file-keep") {
		t.Fatalf("删除前列表 = %v, %v", fileIDs(files), err)
	}
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	files, err = pool.ListFiles(context.Background())
	if err != nil || containsFile(files, "file-gone") || !containsFile(files, "file-keep") {
		t.Fatalf("删除账户后列表 = %v, %v", fileIDs(files), err)
	}
	if _, err := pool.FileMetadata(context.Background(), "file-gone"); !errors.Is(err, ErrResourceNotFound) || strings.Contains(err.Error(), "keep@example.com") {
		t.Fatalf("已删除账户的文件详情 = %v", err)
	}
}

func directoryBase(directory string) string { return filepath.Base(directory) }

func containsFile(files []FileMetadata, id string) bool {
	for _, file := range files {
		if file.ID == id {
			return true
		}
	}
	return false
}
