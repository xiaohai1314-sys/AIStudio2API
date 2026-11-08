package config

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNonstreamKeepaliveDefaultIsOff(t *testing.T) {
	if Default().NonstreamKeepalive {
		t.Fatal("默认开启非流式保活会在 10 秒后提交 200")
	}
	enabled, err := Load(writeTestEnv(t, "WAA_BACKEND=spidermonkey\nNONSTREAM_KEEPALIVE=true\n"))
	if err != nil || !enabled.NonstreamKeepalive {
		t.Fatalf("显式开启没有保留: %+v %v", enabled.NonstreamKeepalive, err)
	}
	disabled, err := Load(writeTestEnv(t, "WAA_BACKEND=spidermonkey\n"))
	if err != nil || disabled.NonstreamKeepalive {
		t.Fatalf("未设置时应使用默认关闭: %+v %v", disabled.NonstreamKeepalive, err)
	}
}

func TestCredentialEncryptionRequiresExternalKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 使用 DPAPI，不读取外部密钥")
	}
	plain := Default()
	if err := plain.Validate(); err != nil {
		t.Fatal(err)
	}

	missing := Default()
	missing.EncryptCredentials = true
	if err := missing.Validate(); err == nil || strings.Contains(err.Error(), "secret-key-material") {
		t.Fatalf("缺少密钥时应拒绝启动，且错误不含密钥内容: %v", err)
	}

	inside := Default()
	inside.EncryptCredentials = true
	inside.CredentialsKeyFile = "credentials.key"
	if err := inside.Validate(); err == nil || !strings.Contains(err.Error(), "工作目录") {
		t.Fatalf("数据目录内的密钥文件应被拒绝: %v", err)
	}

	authDir := t.TempDir()
	authKey := filepath.Join(authDir, "credentials.key")
	if err := os.WriteFile(authKey, bytes.Repeat([]byte{9}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	placedWithAuth := Default()
	placedWithAuth.EncryptCredentials = true
	placedWithAuth.AuthStates = authDir
	placedWithAuth.CredentialsKeyFile = authKey
	if err := placedWithAuth.Validate(); err == nil || !strings.Contains(err.Error(), "账户目录") {
		t.Fatalf("账户目录内的密钥文件应被拒绝: %v", err)
	}

	loosePath := filepath.Join(t.TempDir(), "credentials.key")
	if err := os.WriteFile(loosePath, bytes.Repeat([]byte{4}, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	loose := Default()
	loose.EncryptCredentials = true
	loose.CredentialsKeyFile = loosePath
	if err := loose.Validate(); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("过宽权限应被拒绝: %v", err)
	}

	absent := Default()
	absent.EncryptCredentials = true
	absentPath := filepath.Join(t.TempDir(), "missing.key")
	absent.CredentialsKeyFile = absentPath
	if err := absent.Validate(); err == nil || !strings.Contains(err.Error(), "不会自动生成") {
		t.Fatalf("缺失密钥文件应被拒绝且不创建: %v", err)
	}
	if _, err := os.Stat(absentPath); !os.IsNotExist(err) {
		t.Fatalf("校验创建了密钥文件: %v", err)
	}

	keyPath := filepath.Join(t.TempDir(), "credentials.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{3}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	external := Default()
	external.EncryptCredentials = true
	external.CredentialsKeyFile = keyPath
	if err := external.Validate(); err != nil {
		t.Fatal(err)
	}

	both := external
	both.CredentialsKey = bytes.Repeat([]byte{8}, 32)
	both.Environment = []string{"CREDENTIALS_KEY"}
	if err := both.Validate(); err == nil {
		t.Fatal("同时设置两种密钥来源时应拒绝")
	}
}

func TestCredentialsKeyFromEnvironmentOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 使用 DPAPI，不读取外部密钥")
	}
	secret := bytes.Repeat([]byte{0x2a}, 32)
	encoded := base64.StdEncoding.EncodeToString(secret)
	fileOnly := "WAA_BACKEND=spidermonkey\nENCRYPT_CREDENTIALS=true\nCREDENTIALS_KEY=" + encoded + "\n"
	path := writeTestEnv(t, fileOnly)
	if _, err := Load(path); err == nil || strings.Contains(err.Error(), encoded) {
		t.Fatalf("写在 .env 中的密钥应被拒绝且不回显: %v", err)
	}
	if got := readTestEnv(t, path); got != fileOnly {
		t.Fatalf("拒绝启动时改写了配置:\n%s", got)
	}

	t.Setenv("ENCRYPT_CREDENTIALS", "true")
	t.Setenv("CREDENTIALS_KEY", encoded)
	envPath := writeTestEnv(t, "WAA_BACKEND=spidermonkey\nENCRYPT_CREDENTIALS=false\nCREDENTIALS_KEY=present-in-file\n")
	loaded, err := Load(envPath)
	if err != nil || !bytes.Equal(loaded.CredentialsKey, secret) {
		t.Fatalf("环境变量密钥没有生效: %v len=%d", err, len(loaded.CredentialsKey))
	}
	if err := loaded.Save(envPath); err != nil {
		t.Fatal(err)
	}
	saved := readTestEnv(t, envPath)
	if strings.Contains(saved, "CREDENTIALS_KEY") || strings.Contains(saved, encoded) || strings.Contains(saved, "present-in-file") {
		t.Fatalf("密钥被写回 .env:\n%s", saved)
	}
}
