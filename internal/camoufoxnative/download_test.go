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

func TestInstallCamoufoxArchiveDownloadFailureIncludesRecoveryInstructions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	root := filepath.Join(t.TempDir(), "camoufox")
	err := installCamoufoxArchive(context.Background(), root, filepath.Join(t.TempDir(), "archive.part"), "camoufox", server.URL, "", strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), ".env 的 PROXY") || !strings.Contains(err.Error(), server.URL) || !strings.Contains(err.Error(), root) {
		t.Fatalf("install error = %v, want proxy and manual-download recovery instructions", err)
	}
}

func TestCamoufoxArchiveChecksumsCoverReleaseAssets(t *testing.T) {
	assets := []string{
		"camoufox-158.0.1-beta.41-lin.arm64.zip",
		"camoufox-158.0.1-beta.41-lin.x86_64.zip",
		"camoufox-158.0.1-beta.41-mac.arm64.zip",
		"camoufox-158.0.1-beta.41-mac.x86_64.zip",
		"camoufox-158.0.1-beta.41-win.i686.zip",
		"camoufox-158.0.1-beta.41-win.x86_64.zip",
	}
	if len(camoufoxArchiveSHA256) != len(assets) {
		t.Fatalf("checksum entries = %d, want %d release assets", len(camoufoxArchiveSHA256), len(assets))
	}
	for _, asset := range assets {
		digest, err := camoufoxArchiveChecksum(asset)
		if err != nil {
			t.Errorf("%s: %v", asset, err)
			continue
		}
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size {
			t.Errorf("%s has invalid SHA-256 %q", asset, digest)
		}
	}
	if _, err := camoufoxArchiveChecksum("camoufox-158.0.1-beta.41-lin.i686.zip"); err == nil {
		t.Fatal("asset without an official pinned digest was accepted")
	}
}

func TestInstallCamoufoxArchiveRejectsBadDigestBeforeExtractOrPublish(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "camoufox")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "existing-install")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(parent, "archive.part")
	archive := []byte("tampered archive bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	wrong := sha256.Sum256([]byte("different archive bytes"))
	err := installCamoufoxArchive(context.Background(), root, archivePath, "camoufox", server.URL, "", hex.EncodeToString(wrong[:]))
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("install error = %v, want SHA-256 verification failure", err)
	}
	if strings.Contains(err.Error(), ".env 的 PROXY") {
		t.Fatalf("checksum rejection should not suggest bypassing archive verification: %v", err)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("rejected archive was not removed: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("existing install changed after rejection: data=%q err=%v", data, err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".camoufox-stage-") {
			t.Fatalf("archive was staged before digest verification: %s", entry.Name())
		}
	}
}
