package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// signTestData 用随机密钥按 minisign 格式签名，返回签名文件内容与公钥字符串
func signTestData(t *testing.T, data []byte) (sigFile []byte, pubKey string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID := make([]byte, 8)
	if _, err := rand.Read(keyID); err != nil {
		t.Fatal(err)
	}
	pubRaw := append(append([]byte("Ed"), keyID...), pub...)
	pubKey = base64.StdEncoding.EncodeToString(pubRaw)
	sig := ed25519.Sign(priv, data)
	sigRaw := append(append([]byte("Ed"), keyID...), sig...)
	sigFile = []byte(fmt.Sprintf("untrusted comment: signature from minisign secret key\n%s\ntrusted comment: timestamp:1\tfile:test\n",
		base64.StdEncoding.EncodeToString(sigRaw)))
	return sigFile, pubKey
}

func TestVerifyMinisign(t *testing.T) {
	data := []byte("fake-release-binary")
	sigFile, pubKey := signTestData(t, data)

	// 临时替换内嵌公钥为测试密钥
	old := updateSigningPublicKey
	updateSigningPublicKey = pubKey
	defer func() { updateSigningPublicKey = old }()

	if err := verifyMinisign(data, sigFile); err != nil {
		t.Fatalf("合法签名未通过: %v", err)
	}
	tampered := append(append([]byte{}, data...), 'x')
	if err := verifyMinisign(tampered, sigFile); err == nil {
		t.Fatal("篡改后的内容通过了校验")
	}
	if err := verifyMinisign(data, []byte("not a signature")); err == nil {
		t.Fatal("非法签名文件通过了校验")
	}
	// 密钥 ID 不匹配
	otherSig, otherPub := signTestData(t, data)
	_ = otherPub
	lines := strings.Split(string(otherSig), "\n")
	badKeySig := []byte("untrusted comment: x\n" + lines[1] + "\n")
	if err := verifyMinisign(data, badKeySig); err == nil {
		t.Fatal("密钥 ID 不匹配的签名通过了校验")
	}
}

func TestEmbeddedPublicKeyFormat(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(updateSigningPublicKey)
	if err != nil || len(raw) != 2+8+ed25519.PublicKeySize || string(raw[:2]) != "Ed" {
		t.Fatalf("内嵌签名公钥格式错误: %v", err)
	}
}
