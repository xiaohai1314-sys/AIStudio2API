package camoufoxnative

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

const camoufoxRelease = "152.0.4-beta.29"

// camoufoxChecksums 是固定发行版各平台压缩包的 sha256，升级 camoufoxRelease 时须同步更新
var camoufoxChecksums = map[string]string{
	"camoufox-152.0.4-beta.29-lin.arm64.zip":  "c8844c03f3c233d6a466d59fd3f4e6f21470bd685e1b7cc82d650379a9b23e35",
	"camoufox-152.0.4-beta.29-lin.x86_64.zip": "1bea4b55a51c88e82dc7d426d9c75093d942d2afc8c911cb8fc78ebf723d686c",
	"camoufox-152.0.4-beta.29-mac.arm64.zip":  "620d33289b5d52154cc7d92a9cca5915329aa6684105ab0f371d01480768eb53",
	"camoufox-152.0.4-beta.29-mac.x86_64.zip": "eeada250746edefc5d7b63fc371d51ed3252b21134e971ac64ca590ff78e7bb7",
	"camoufox-152.0.4-beta.29-win.i686.zip":   "fc98bab1836a61f0cff88731bfa3a170a839c5b61f93d581e3844d2c4adc3fc9",
	"camoufox-152.0.4-beta.29-win.x86_64.zip": "b9ccdc298330e96f807999fe22aa212e9847fc9e210bc36b2e965b6cce03b25b",
}

// Download 是正在进行的 Camoufox 下载的已收与总字节，总字节未知时为 0
type Download struct {
	Received int64 `json:"received"`
	Total    int64 `json:"total"`
}

// downloadState 是正在进行的下载的计数，写入时累计已收字节
type downloadState struct {
	received atomic.Int64
	total    int64
}

func (state *downloadState) Write(data []byte) (int, error) {
	state.received.Add(int64(len(data)))
	return len(data), nil
}

// activeDownload 是正在进行的 Camoufox 下载，没有下载时为 nil
var activeDownload atomic.Pointer[downloadState]

// DownloadProgress 返回正在进行的 Camoufox 下载，没有下载时 ok 为 false
func DownloadProgress() (Download, bool) {
	state := activeDownload.Load()
	if state == nil {
		return Download{}, false
	}
	return Download{Received: state.received.Load(), Total: state.total}, true
}

// installCamoufox 经出口代理下载当前协议传输已对齐的 Camoufox 版本
func installCamoufox(ctx context.Context, executableName string, proxy string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	asset, err := camoufoxAssetName()
	if err != nil {
		return "", err
	}
	expected, ok := camoufoxChecksums[asset]
	if !ok {
		return "", fmt.Errorf("Camoufox %s 没有内置的 sha256 摘要，无法校验下载内容", asset)
	}
	root, err := camoufoxInstallRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return "", fmt.Errorf("创建 Camoufox 目录: %w", err)
	}
	archivePath := filepath.Join(filepath.Dir(root), asset+".part")
	address := fmt.Sprintf("https://github.com/daijro/camoufox/releases/download/v%s/%s", camoufoxRelease, asset)
	if err := downloadCamoufoxArchive(ctx, address, archivePath, proxy); err != nil {
		if ctx.Err() != nil {
			return "", err
		}
		return "", fmt.Errorf("%w；在 .env 的 PROXY 设置能访问 GitHub 的代理后重试，或手动下载 %s 解压到 %s", err, address, root)
	}
	if err := verifyCamoufoxArchive(archivePath, expected); err != nil {
		os.Remove(archivePath)
		return "", err
	}
	slog.Info("Camoufox 下载校验通过", "sha256", expected)
	staging, err := os.MkdirTemp(filepath.Dir(root), ".camoufox-stage-*")
	if err != nil {
		return "", fmt.Errorf("创建 Camoufox 临时目录: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := extractCamoufoxArchive(ctx, archivePath, staging); err != nil {
		if ctx.Err() == nil {
			os.Remove(archivePath)
		}
		return "", err
	}
	stagedExecutable := filepath.Join(staging, executableName)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(stagedExecutable, 0o755); err != nil {
			return "", fmt.Errorf("设置 Camoufox 执行权限: %w", err)
		}
	}
	if _, err := validateCamoufoxExecutable(stagedExecutable); err != nil {
		return "", fmt.Errorf("校验 Camoufox 临时目录: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := replaceCamoufoxRoot(root, staging); err != nil {
		return "", err
	}
	os.Remove(archivePath)
	executable := filepath.Join(root, executableName)
	slog.Info("Camoufox 已就绪", "path", executable)
	return executable, nil
}

// replaceCamoufoxRoot 用 staging 替换 root：先把旧目录改名备份，发布失败时恢复，避免升级中断后失去可用版本
func replaceCamoufoxRoot(root, staging string) error {
	backup := root + ".old"
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("清理旧 Camoufox 备份: %w", err)
	}
	if _, err := os.Stat(root); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("检查 Camoufox 目录: %w", err)
		}
		return os.Rename(staging, root)
	}
	if err := os.Rename(root, backup); err != nil {
		return fmt.Errorf("移动旧 Camoufox 目录: %w；旧版本可能仍在运行，请退出 Camoufox 后重试", err)
	}
	if err := os.Rename(staging, root); err != nil {
		if restoreErr := os.Rename(backup, root); restoreErr != nil {
			return fmt.Errorf("发布 Camoufox 目录: %w；恢复旧目录失败: %v，旧目录在 %s", err, restoreErr, backup)
		}
		return fmt.Errorf("发布 Camoufox 目录: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		slog.Warn("清理旧 Camoufox 目录失败", "path", backup, "error", err)
	}
	return nil
}

// downloadCamoufoxArchive 经出口代理把发行包写入 archivePath，文件已有部分内容时从末尾续传
func downloadCamoufoxArchive(ctx context.Context, address string, archivePath string, proxy string) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	egress := "直连"
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return fmt.Errorf("解析 Camoufox 下载代理: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		egress = proxyURL.Redacted()
	}
	var offset int64
	if info, err := os.Stat(archivePath); err == nil {
		offset = info.Size()
	}
	slog.Info("正在下载 Camoufox", "version", camoufoxRelease, "platform", runtime.GOOS+"/"+runtime.GOARCH, "egress", egress)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "AIStudio2API")
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	response, err := (&http.Client{Timeout: 30 * time.Minute, Transport: transport}).Do(request)
	if err != nil {
		return fmt.Errorf("下载 Camoufox: %w", err)
	}
	defer response.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch response.StatusCode {
	case http.StatusOK:
		offset = 0
		flags |= os.O_TRUNC
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusRequestedRangeNotSatisfiable:
		return nil
	default:
		return fmt.Errorf("下载 Camoufox: HTTP %d", response.StatusCode)
	}
	state := &downloadState{}
	state.received.Store(offset)
	if response.ContentLength > 0 {
		state.total = offset + response.ContentLength
		slog.Info("Camoufox 下载已开始", "size_mib", state.total/(1024*1024), "resume_mib", offset/(1024*1024))
	}
	archive, err := os.OpenFile(archivePath, flags, 0o644)
	if err != nil {
		return fmt.Errorf("创建 Camoufox 下载文件: %w", err)
	}
	activeDownload.Store(state)
	defer activeDownload.Store(nil)
	_, copyErr := io.Copy(io.MultiWriter(archive, state), contextReader{ctx: ctx, reader: response.Body})
	if closeErr := archive.Close(); copyErr != nil || closeErr != nil {
		return fmt.Errorf("保存 Camoufox: %w", firstError(copyErr, closeErr))
	}
	return nil
}

func camoufoxInstallRoot() (string, error) {
	root, err := filepath.Abs(filepath.Join("runtime", "camoufox"))
	if err != nil {
		return "", fmt.Errorf("定位 Camoufox 目录: %w", err)
	}
	return root, nil
}

func camoufoxAssetName() (string, error) {
	platform := map[string]string{"windows": "win", "linux": "lin", "darwin": "mac"}[runtime.GOOS]
	architecture := map[string]string{"amd64": "x86_64", "386": "i686", "arm64": "arm64"}[runtime.GOARCH]
	if platform == "" || architecture == "" || runtime.GOOS == "darwin" && runtime.GOARCH == "386" {
		return "", fmt.Errorf("Camoufox 没有 %s/%s 发行包", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("camoufox-%s-%s.%s.zip", camoufoxRelease, platform, architecture), nil
}

// verifyCamoufoxArchive 校验下载完成的压缩包与内置 sha256 一致
func verifyCamoufoxArchive(archivePath string, expected string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("校验 Camoufox 下载文件: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("校验 Camoufox 下载文件: %w", err)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(actual, expected) {
		return fmt.Errorf("Camoufox 下载文件校验失败：期望 sha256 %s，实际 %s", expected, actual)
	}
	return nil
}

func extractCamoufoxArchive(ctx context.Context, archivePath string, destination string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开 Camoufox 压缩包: %w", err)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(entry.Name))
		relative, err := filepath.Rel(destination, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("Camoufox 压缩包包含无效路径 %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, entry.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, entry.Mode())
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(targetFile, contextReader{ctx: ctx, reader: source})
		closeTargetErr := targetFile.Close()
		closeSourceErr := source.Close()
		if copyErr != nil || closeTargetErr != nil || closeSourceErr != nil {
			return fmt.Errorf("解压 Camoufox %s: %w", entry.Name, firstError(copyErr, closeTargetErr, closeSourceErr))
		}
	}
	return nil
}

// contextReader 在复制过程中传播装配取消
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

// Read 在每个数据块前检查装配取消
func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func firstError(values ...error) error {
	for _, err := range values {
		if err != nil {
			return err
		}
	}
	return nil
}
