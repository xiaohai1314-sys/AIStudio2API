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
		// 创建时设 0600，读取时也要校验：从备份还原等途径预置的宽权限文件不能静默沿用
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("凭证密钥文件权限过宽 (%04o)，应为 0600: %s", info.Mode().Perm(), path)
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
	// 先写临时文件再原子发布，避免中断后留下长度错误的密钥；已有密钥时使用先创建的一方
	temp, err := os.CreateTemp(filepath.Dir(path), ".credentials-key-*")
	if err != nil {
		return nil, fmt.Errorf("创建凭证密钥临时文件: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return nil, fmt.Errorf("设置凭证密钥权限: %w", err)
	}
	if _, err := temp.Write(key); err != nil {
		temp.Close()
		return nil, fmt.Errorf("写入凭证密钥: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return nil, fmt.Errorf("同步凭证密钥: %w", err)
	}
	if err := temp.Close(); err != nil {
		return nil, fmt.Errorf("关闭凭证密钥临时文件: %w", err)
	}
	if err := os.Link(tempName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readExistingKey(path)
		}
		// 不支持硬链接的文件系统改用排他创建发布：Rename 在 POSIX 下会直接覆盖目标，
		// 并发首次启动时可能把对方已发布的密钥换掉，使其已加密的凭证无法解密
		published, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr != nil {
			if errors.Is(openErr, os.ErrExist) {
				return readExistingKey(path)
			}
			return nil, fmt.Errorf("发布凭证密钥: %w", openErr)
		}
		if _, writeErr := published.Write(key); writeErr != nil {
			published.Close()
			os.Remove(path)
			return nil, fmt.Errorf("发布凭证密钥: %w", writeErr)
		}
		if syncErr := published.Sync(); syncErr != nil {
			published.Close()
			os.Remove(path)
			return nil, fmt.Errorf("发布凭证密钥: %w", syncErr)
		}
		if closeErr := published.Close(); closeErr != nil {
			os.Remove(path)
			return nil, fmt.Errorf("发布凭证密钥: %w", closeErr)
		}
	}
	return key, nil
}

// readExistingKey 读取并发创建的一方写入的密钥
func readExistingKey(path string) ([]byte, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取并发创建的凭证密钥: %w", err)
	}
	if len(existing) != aesKeySize {
		return nil, fmt.Errorf("凭证密钥文件长度应为 %d 字节", aesKeySize)
	}
	return existing, nil
}
