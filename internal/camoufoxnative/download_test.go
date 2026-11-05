package camoufoxnative

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestVerifyCamoufoxArchive 验证摘要一致时通过、不一致或文件缺失时报错
func TestVerifyCamoufoxArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.zip")
	data := []byte("camoufox")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := verifyCamoufoxArchive(path, hex.EncodeToString(sum[:])); err != nil {
		t.Errorf("一致摘要校验失败: %v", err)
	}
	if err := verifyCamoufoxArchive(path, strings.Repeat("0", 64)); err == nil {
		t.Error("不一致摘要被接受")
	}
	if err := verifyCamoufoxArchive(filepath.Join(t.TempDir(), "missing.zip"), strings.Repeat("0", 64)); err == nil {
		t.Error("缺失文件被接受")
	}
}

func TestDownloadCamoufoxArchiveRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(maxCamoufoxArchiveBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	archivePath := filepath.Join(t.TempDir(), "archive.part")
	err := downloadCamoufoxArchive(context.Background(), server.URL, archivePath, "")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("download error = %v, want oversized archive error", err)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("oversized archive was left on disk: %v", err)
	}
}

// TestCamoufoxChecksums 验证内置摘要覆盖固定发行版的全部平台产物
func TestCamoufoxChecksums(t *testing.T) {
	for _, platform := range []string{"lin.arm64", "lin.x86_64", "mac.arm64", "mac.x86_64", "win.i686", "win.x86_64"} {
		name := "camoufox-" + camoufoxRelease + "-" + platform + ".zip"
		digest, ok := camoufoxChecksums[name]
		if !ok {
			t.Errorf("%s 缺少内置摘要", name)
			continue
		}
		if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
			t.Errorf("%s 的摘要 %q 不是小写十六进制 sha256", name, digest)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			t.Errorf("%s 的摘要 %q 不是十六进制: %v", name, digest, err)
		}
	}
}
