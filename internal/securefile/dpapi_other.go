//go:build !windows

package securefile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// aesKeySize 是 AES-256 密钥的字节数
const aesKeySize = 32

// errUnsupported 表示当前平台没有 DPAPI 或缺少密钥配置
var errUnsupported = errors.New("凭证加密需要配置密钥文件")

var keyMu sync.Mutex

func envelope() string { return aesEnvelopePrefix }

func protect(data []byte) ([]byte, error) { return sealAES(data) }

func unprotect([]byte) ([]byte, error) {
	return nil, errors.New("DPAPI 凭证加密仅支持 Windows")
}

func unprotectAES(data []byte) ([]byte, error) { return openAES(data) }

// sealAES 用 AES-256-GCM 加密并前置随机 nonce
func sealAES(data []byte) ([]byte, error) {
	aead, err := credentialCipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("生成凭证随机数: %w", err)
	}
	return aead.Seal(nonce, nonce, data, nil), nil
}

// openAES 校验 nonce 长度并解密
func openAES(data []byte) ([]byte, error) {
	aead, err := credentialCipher()
	if err != nil {
		return nil, err
	}
	if len(data) < aead.NonceSize() {
		return nil, errors.New("凭证密文长度无效")
	}
	return aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
}

func credentialCipher() (cipher.AEAD, error) {
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("初始化凭证密钥: %w", err)
	}
	return cipher.NewGCM(block)
}

// loadKey 读取或创建权限为 0600 的 AES 密钥文件
func loadKey() ([]byte, error) {
	path := configuredKeyPath()
	if path == "" {
		return nil, errUnsupported
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(data) != aesKeySize {
			return nil, fmt.Errorf("凭证密钥文件长度应为 %d 字节", aesKeySize)
		}
		return data, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("读取凭证密钥: %w", err)
	}
	key := make([]byte, aesKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("生成凭证密钥: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建凭证密钥目录: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("写入凭证密钥: %w", err)
	}
	return key, nil
}
