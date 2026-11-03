//go:build !windows

package securefile

import (
	"bytes"
	"path/filepath"
	"testing"
)

// TestAESRoundTrip 验证非 Windows 平台用密钥文件加解密凭证
func TestAESRoundTrip(t *testing.T) {
	ConfigureKeyPath(filepath.Join(t.TempDir(), "credentials.key"))
	Configure(true)
	defer Configure(false)
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

// TestAESRequiresKeyPath 验证未配置密钥文件时拒绝加密
func TestAESRequiresKeyPath(t *testing.T) {
	ConfigureKeyPath("")
	Configure(true)
	defer Configure(false)
	if _, err := Encrypt([]byte("secret")); err == nil {
		t.Fatal("缺少密钥文件时仍然加密成功")
	}
}
