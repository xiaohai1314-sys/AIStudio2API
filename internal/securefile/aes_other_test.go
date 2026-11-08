//go:build !windows

package securefile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeTestKey(t *testing.T, path string) {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, aesKeySize)
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
}

func resetCredentialKey(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		Configure(false)
		ConfigureKey(nil)
		ConfigureKeyPath("")
	})
}

// TestAESRoundTrip 验证非 Windows 平台用已准备好的密钥文件加解密凭证
func TestAESRoundTrip(t *testing.T) {
	resetCredentialKey(t)
	path := filepath.Join(t.TempDir(), "credentials.key")
	writeTestKey(t, path)
	ConfigureKeyPath(path)
	Configure(true)
	data := []byte(`{"cookies":[{"name":"SID","value":"secret"}]}`)
	encrypted, err := Encrypt(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(encrypted, []byte(aesEnvelopePrefix)) || bytes.Contains(encrypted, []byte("secret")) {
		t.Fatalf("内容没有被加密: %q", encrypted)
	}
	decrypted, err := Decrypt(encrypted)
	if err != nil || !bytes.Equal(decrypted, data) {
		t.Fatalf("解密结果 = %q, %v", decrypted, err)
	}
}

// TestAESInlineKeyRoundTrip 验证进程内密钥可以加解密，并且不创建密钥文件
func TestAESInlineKeyRoundTrip(t *testing.T) {
	resetCredentialKey(t)
	ConfigureKeyPath("")
	ConfigureKey(bytes.Repeat([]byte{0x11}, aesKeySize))
	Configure(true)
	encrypted, err := Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := Decrypt(encrypted)
	if err != nil || string(decrypted) != "secret" {
		t.Fatalf("解密结果 = %q, %v", decrypted, err)
	}
}

// TestAESMissingKeyIsNotCreated 验证缺少密钥文件时加密失败，且不会把新密钥写到调用方给出的路径
func TestAESMissingKeyIsNotCreated(t *testing.T) {
	resetCredentialKey(t)
	path := filepath.Join(t.TempDir(), "credentials.key")
	ConfigureKey(nil)
	ConfigureKeyPath(path)
	Configure(true)
	if _, err := Encrypt([]byte("secret")); err == nil {
		t.Fatal("缺少密钥文件时仍然加密成功")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("缺失的密钥文件被创建了: %v", err)
	}
}

// TestAESRejectsLoosePermissions 验证组或其他用户可读的密钥文件被拒绝
func TestAESRejectsLoosePermissions(t *testing.T) {
	resetCredentialKey(t)
	path := filepath.Join(t.TempDir(), "credentials.key")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0x7}, aesKeySize), 0o644); err != nil {
		t.Fatal(err)
	}
	ConfigureKey(nil)
	ConfigureKeyPath(path)
	Configure(true)
	if _, err := Encrypt([]byte("secret")); err == nil {
		t.Fatal("权限过宽的密钥文件仍然可以加密")
	}
}

// TestAESPlaintextRemainsReadable 验证开启加密后，已有明文仍可读取，解密不会改写调用方持有的原文
func TestAESPlaintextRemainsReadable(t *testing.T) {
	resetCredentialKey(t)
	ConfigureKey(bytes.Repeat([]byte{0x3}, aesKeySize))
	Configure(true)
	plaintext := []byte(`{"cookies":[{"name":"SID","value":"legacy"}]}`)
	stored := append([]byte(nil), plaintext...)
	decrypted, err := Decrypt(stored)
	if err != nil || !bytes.Equal(decrypted, plaintext) || !bytes.Equal(stored, plaintext) {
		t.Fatalf("明文迁移读取 = %q stored=%q err=%v", decrypted, stored, err)
	}
}

// TestAESLostKeyDoesNotRevealOrRewrite 验证密钥不符时解密失败，磁盘上的密文保持原样
func TestAESLostKeyDoesNotRevealOrRewrite(t *testing.T) {
	resetCredentialKey(t)
	path := filepath.Join(t.TempDir(), "storage-state.json")
	ConfigureKey(bytes.Repeat([]byte{0x5}, aesKeySize))
	Configure(true)
	encrypted, err := Encrypt([]byte("secret-cookie"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encrypted, 0o600); err != nil {
		t.Fatal(err)
	}
	ConfigureKey(bytes.Repeat([]byte{0x6}, aesKeySize))
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(stored); err == nil {
		t.Fatal("错误密钥仍然解密成功")
	}
	again, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(again, encrypted) {
		t.Fatalf("密文文件被改写: %v", err)
	}
}

// TestAESRequiresKeyPath 验证未配置密钥来源时拒绝加密
func TestAESRequiresKeyPath(t *testing.T) {
	resetCredentialKey(t)
	ConfigureKey(nil)
	ConfigureKeyPath("")
	Configure(true)
	if _, err := Encrypt([]byte("secret")); err == nil {
		t.Fatal("缺少密钥文件时仍然加密成功")
	}
}

// TestAESRejectsWrongKeyLength 验证长度错误的密钥文件被拒绝，文件内容保持不变
func TestAESRejectsWrongKeyLength(t *testing.T) {
	resetCredentialKey(t)
	path := filepath.Join(t.TempDir(), "credentials.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	ConfigureKey(nil)
	ConfigureKeyPath(path)
	Configure(true)
	if _, err := Encrypt([]byte("secret")); err == nil {
		t.Fatal("长度错误的密钥文件未被拒绝")
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != "short" {
		t.Fatalf("密钥文件被改写: %q %v", stored, err)
	}
}
