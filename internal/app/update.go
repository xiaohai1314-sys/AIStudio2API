package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/mod/semver"

	"github.com/Mag1cFall/AIStudio2API/internal/api"
)

// releaseAPI 为 GitHub 最新发行版本接口（指向本 fork，不跟随上游）
const releaseAPI = "https://api.github.com/repos/xiaohai1314-sys/AIStudio2API/releases/latest"

// updateSigningPublicKey 是发行包签名的 minisign 公钥，
// base64 编码内容为：签名算法标识 "Ed"(2 字节) || 密钥 ID(8 字节) || Ed25519 公钥(32 字节)。
// 私钥由仓库维护者在 GitHub Secrets(MINISIGN_PRIVATE_KEY) 中保管，CI 在发布时为每个发行包生成同名的 .minisig 签名文件。
// Key ID: bdc920bca9dd67f2
const updateSigningPublicKey = "RWS9ySC8qd1n8mXKOV/iNbNCNNR9cLGaQNhRH2tLAtGTCBTTrXNz2iyR"

// releaseCheckInterval 为最新发行版本的缓存时长
const releaseCheckInterval = time.Hour

// githubRelease 为 GitHub 发行版本接口中用到的字段
type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

// githubAsset 为发行版本中的一个文件
type githubAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

// updateConflictError 表示当前状态不能安装更新
type updateConflictError string

func (err updateConflictError) Error() string { return string(err) }

// HTTPStatus 返回更新冲突的 HTTP 状态码
func (updateConflictError) HTTPStatus() int { return http.StatusConflict }

// selfUpdater 读取最新发行版本并替换当前可执行文件
type selfUpdater struct {
	version    string
	mode       string
	releaseURL string
	executable string
	proxy      func() string
	log        func(level, message string)
	restart    func()
	mu         sync.Mutex
	release    *githubRelease
	checked    time.Time
	checkErr   error
	updating   bool
}

// newSelfUpdater 按构建版本与运行环境创建替换 executable 的更新器
func newSelfUpdater(version, executable string, proxy func() string, log func(level, message string), restart func()) *selfUpdater {
	return &selfUpdater{
		version: version, mode: updateMode(version), releaseURL: releaseAPI, executable: executable,
		proxy: proxy, log: log, restart: restart,
	}
}

// updateMode 返回更新方式：managed 由分发方更新，binary 可原地替换，container 随镜像更新，development 为源码构建
func updateMode(version string) string {
	if managedBy != "" {
		return "managed"
	}
	if !semver.IsValid(version) || semver.Prerelease(version) != "" || semver.Build(version) != "" {
		return "development"
	}
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return "container"
		}
	}
	return platformUpdateMode()
}

// UpdateStatus 返回当前版本与最新发行版本，refresh 为 true 或缓存过期时重新读取
func (u *selfUpdater) UpdateStatus(ctx context.Context, refresh bool) (api.UpdateStatus, error) {
	u.mu.Lock()
	stale := u.mode != "managed" && (refresh || time.Since(u.checked) >= releaseCheckInterval)
	u.mu.Unlock()
	if stale {
		release, err := u.fetchRelease(ctx)
		if ctx.Err() != nil {
			return api.UpdateStatus{}, ctx.Err()
		}
		u.mu.Lock()
		u.checked, u.checkErr = time.Now(), err
		if err == nil {
			u.release = release
		}
		u.mu.Unlock()
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.statusLocked(), nil
}

// ApplyUpdate 下载最新发行包，校验摘要后替换当前可执行文件并重启管理进程
func (u *selfUpdater) ApplyUpdate(ctx context.Context) (api.UpdateStatus, error) {
	u.mu.Lock()
	if u.mode == "managed" {
		u.mu.Unlock()
		return api.UpdateStatus{}, updateConflictError("在线更新由 " + managedBy + " 管理")
	}
	if u.mode != "binary" {
		u.mu.Unlock()
		return api.UpdateStatus{}, updateConflictError("当前运行方式不支持在线更新")
	}
	if u.updating {
		u.mu.Unlock()
		return api.UpdateStatus{}, updateConflictError("更新正在进行")
	}
	u.updating = true
	u.mu.Unlock()
	release, err := u.fetchRelease(ctx)
	if err == nil && semver.Compare(release.TagName, u.version) <= 0 {
		err = updateConflictError("当前已是最新版本 " + u.version)
	}
	if err == nil {
		u.log("INFO", "在线更新 | 1/2 | 下载发行包 | 版本="+release.TagName)
		err = u.install(ctx, release)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		u.updating = false
		u.log("ERROR", "在线更新失败 | 错误="+err.Error())
		return api.UpdateStatus{}, err
	}
	u.release, u.checked, u.checkErr = release, time.Now(), nil
	u.log("INFO", "在线更新完成 | 版本="+release.TagName)
	status := u.statusLocked()
	u.restart()
	return status, nil
}

// statusLocked 在持有锁时生成更新状态
func (u *selfUpdater) statusLocked() api.UpdateStatus {
	status := api.UpdateStatus{Current: u.version, Mode: u.mode, ManagedBy: managedBy, CheckedAt: u.checked, Updating: u.updating}
	if u.checkErr != nil {
		status.Error = u.checkErr.Error()
	}
	if release := u.release; release != nil {
		status.Latest, status.Notes, status.URL, status.PublishedAt = release.TagName, release.Body, release.HTMLURL, release.PublishedAt
		status.Available = semver.Compare(release.TagName, u.version) > 0
	}
	return status
}

// fetchRelease 读取 GitHub 最新发行版本
func (u *selfUpdater) fetchRelease(ctx context.Context) (*githubRelease, error) {
	response, err := u.get(ctx, u.releaseURL, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("读取最新发行版本: %w", err)
	}
	defer response.Body.Close()
	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("解析最新发行版本: %w", err)
	}
	return &release, nil
}

// download 下载 release 中名为 name 的发行包：先用内嵌公钥校验同名的 .minisig
// 签名（缺失或无效都拒绝安装），再按发行版本记录的 SHA-256 校验完整性。
// 签名防的是发行仓库被入侵后资产与摘要一并被替换，摘要防的是传输损坏。
func (u *selfUpdater) download(ctx context.Context, release *githubRelease, name string) ([]byte, error) {
	index := slices.IndexFunc(release.Assets, func(asset githubAsset) bool { return asset.Name == name })
	if index < 0 {
		return nil, fmt.Errorf("发行版本 %s 没有 %s", release.TagName, name)
	}
	asset := release.Assets[index]
	response, err := u.get(ctx, asset.URL, "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("下载 %s: %w", name, err)
	}
	archive, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("下载 %s: %w", name, err)
	}
	sigIndex := slices.IndexFunc(release.Assets, func(asset githubAsset) bool { return asset.Name == name+".minisig" })
	if sigIndex < 0 {
		return nil, fmt.Errorf("发行版本 %s 的 %s 缺少签名文件 %s.minisig，拒绝安装", release.TagName, name, name)
	}
	sigResponse, err := u.get(ctx, release.Assets[sigIndex].URL, "text/plain")
	if err != nil {
		return nil, fmt.Errorf("下载 %s.minisig: %w", name, err)
	}
	sigData, err := io.ReadAll(io.LimitReader(sigResponse.Body, 1024))
	sigResponse.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("下载 %s.minisig: %w", name, err)
	}
	if err := verifyMinisign(archive, sigData, updateSigningPublicKey); err != nil {
		return nil, fmt.Errorf("发行包 %s 签名校验失败: %w", name, err)
	}
	expected, ok := strings.CutPrefix(asset.Digest, "sha256:")
	if !ok {
		return nil, fmt.Errorf("发行包 %s 缺少 SHA-256 摘要", name)
	}
	if sum := sha256.Sum256(archive); hex.EncodeToString(sum[:]) != expected {
		return nil, fmt.Errorf("发行包 %s 的 SHA-256 与发行版本记录不一致", name)
	}
	return archive, nil
}

// verifyMinisign 用 pubKey 验证 data 的 minisign 签名文件。
// 只校验"签名行"（对文件内容本身的 Ed25519 签名），不校验可信注释的时间戳行。
func verifyMinisign(data, sigFile []byte, pubKey string) error {
	const sigAlgEd25519 = "\x45\x64" // minisign 的 Ed25519 算法标识 "Ed"
	pubRaw, err := base64.StdEncoding.DecodeString(pubKey)
	if err != nil || len(pubRaw) != 2+8+ed25519.PublicKeySize || string(pubRaw[:2]) != sigAlgEd25519 {
		return fmt.Errorf("内嵌签名公钥格式错误")
	}
	keyID, publicKey := pubRaw[2:10], ed25519.PublicKey(pubRaw[10:])
	lines := strings.Split(string(sigFile), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "untrusted comment:") {
		return fmt.Errorf("签名文件格式错误")
	}
	sigRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(sigRaw) != 2+8+ed25519.SignatureSize || string(sigRaw[:2]) != sigAlgEd25519 {
		return fmt.Errorf("签名文件格式错误")
	}
	if string(sigRaw[2:10]) != string(keyID) {
		return fmt.Errorf("签名密钥 ID 与内嵌公钥不匹配")
	}
	if !ed25519.Verify(publicKey, data, sigRaw[10:]) {
		return fmt.Errorf("签名无效")
	}
	return nil
}

// get 经管理进程出口发出 GET，非 200 响应返回错误
func (u *selfUpdater) get(ctx context.Context, address, accept string) (*http.Response, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy := u.proxy(); proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("解析代理: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		response.Body.Close()
		return nil, fmt.Errorf("HTTP %d %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return response, nil
}

// extractExecutable 从 zip 或 tar.gz 发行包中取出可执行文件
func extractExecutable(archive []byte, name, binary string) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("读取发行包: %w", err)
		}
		for _, file := range reader.File {
			if path.Base(file.Name) != binary {
				continue
			}
			opened, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("读取发行包: %w", err)
			}
			defer opened.Close()
			return io.ReadAll(opened)
		}
		return nil, fmt.Errorf("发行包中没有 %s", binary)
	}
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("读取发行包: %w", err)
	}
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("发行包中没有 %s", binary)
		}
		if err != nil {
			return nil, fmt.Errorf("读取发行包: %w", err)
		}
		if header.Typeflag == tar.TypeReg && path.Base(header.Name) == binary {
			return io.ReadAll(reader)
		}
	}
}

// relaunch 以原参数启动替换后的可执行文件：Windows 上新进程沿用当前控制台，其他系统原地替换进程
func relaunch(executable string, args []string) error {
	args = append(slices.Clone(args), "-open-ui=false")
	if runtime.GOOS == "windows" {
		command := exec.Command(executable, args...)
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		return command.Start()
	}
	return syscall.Exec(executable, append([]string{executable}, args...), os.Environ())
}
