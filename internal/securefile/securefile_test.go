package securefile

import (
	"bytes"
	"runtime"
	"testing"
)

// TestEncryptDisabledPassthrough 验证未启用时原样读写
func TestEncryptDisabledPassthrough(t *testing.T) {
	Configure(false)
	defer Configure(false)
	data := []byte(`{"cookies":[]}`)
	encrypted, err := Encrypt(data)
	if err != nil || !bytes.Equal(encrypted, data) {
		t.Fatalf("Encrypt() = %q, %v", encrypted, err)
	}
	decrypted, err := Decrypt(data)
	if err != nil || !bytes.Equal(decrypted, data) {
		t.Fatalf("Decrypt() = %q, %v", decrypted, err)
	}
}

// TestDPAPIRoundTrip 验证启用后写出的是密文且能还原
func TestDPAPIRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI 仅在 Windows 上可用")
	}
	Configure(true)
	defer Configure(false)
	data := []byte(`{"cookies":[{"name":"SID","value":"secret"}]}`)
	encrypted, err := Encrypt(data)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encrypted, data) || !bytes.HasPrefix(encrypted, []byte(envelopePrefix)) {
		t.Fatalf("内容没有被加密: %q", encrypted)
	}
	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, data) {
		t.Fatalf("解密结果 = %q", decrypted)
	}
	if plaintext, err := Decrypt([]byte(`{"cookies":[]}`)); err != nil || string(plaintext) != `{"cookies":[]}` {
		t.Fatalf("明文兼容读取 = %q, %v", plaintext, err)
	}
}

// TestDPAPITamperDetected 验证被篡改的密文无法解密
func TestDPAPITamperDetected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI 仅在 Windows 上可用")
	}
	Configure(true)
	defer Configure(false)
	encrypted, err := Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), encrypted...)
	tampered[len(tampered)-2] ^= 0x01
	if _, err := Decrypt(tampered); err == nil {
		t.Fatal("被篡改的密文没有报错")
	}
}
