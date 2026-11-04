package camoufoxnative

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReplaceCamoufoxRoot 验证新目录发布后旧目录被替换并清理
func TestReplaceCamoufoxRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "camoufox")
	staging := filepath.Join(parent, "staging")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceCamoufoxRoot(root, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Fatalf("新目录未发布: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("旧文件仍在发布后的目录中")
	}
	if _, err := os.Stat(root + ".old"); !os.IsNotExist(err) {
		t.Fatal("备份目录未清理")
	}
}

// TestReplaceCamoufoxRootWithoutExisting 验证没有旧目录时直接发布
func TestReplaceCamoufoxRootWithoutExisting(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "camoufox")
	staging := filepath.Join(parent, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceCamoufoxRoot(root, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Fatalf("新目录未发布: %v", err)
	}
}
