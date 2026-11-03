// Package securefile 加密凭证文件：Windows 使用 DPAPI，其他平台使用 AES-256-GCM
package securefile

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"sync/atomic"
)

// envelopePrefix 标记 DPAPI 加密的凭证内容，普通 JSON 与 .env 文本不会以它开头
const envelopePrefix = "AISTUDIO2API-DPAPI1:"

// aesEnvelopePrefix 标记 AES-256-GCM 加密的凭证内容
const aesEnvelopePrefix = "AISTUDIO2API-AESGCM1:"

var (
	enabled atomic.Bool
	keyPath atomic.Value
)

// Configure 设置写出凭证文件时是否加密，进程启动时调用一次
func Configure(value bool) {
	enabled.Store(value)
}

// ConfigureKeyPath 设置非 Windows 平台 AES 密钥文件路径，进程启动时调用一次
func ConfigureKeyPath(path string) {
	keyPath.Store(strings.TrimSpace(path))
}

// configuredKeyPath 返回 AES 密钥文件路径，未配置时为空
func configuredKeyPath() string {
	path, _ := keyPath.Load().(string)
	return path
}

// Enabled 返回是否启用凭证加密
func Enabled() bool {
	return enabled.Load()
}

// Encrypt 在启用时加密内容并加上标记；未启用时原样返回
func Encrypt(data []byte) ([]byte, error) {
	if !enabled.Load() {
		return data, nil
	}
	protected, err := protect(data)
	if err != nil {
		return nil, fmt.Errorf("加密凭证: %w", err)
	}
	return []byte(envelope() + base64.StdEncoding.EncodeToString(protected)), nil
}

// Decrypt 解密带标记的凭证内容；未加密的文件原样返回，便于从明文平滑迁移
func Decrypt(data []byte) ([]byte, error) {
	if encoded, ok := bytes.CutPrefix(data, []byte(envelopePrefix)); ok {
		return decodeEnvelope(encoded, unprotect)
	}
	if encoded, ok := bytes.CutPrefix(data, []byte(aesEnvelopePrefix)); ok {
		return decodeEnvelope(encoded, unprotectAES)
	}
	return data, nil
}

// decodeEnvelope 解码 base64 信封并调用对应的解密实现
func decodeEnvelope(encoded []byte, decrypt func([]byte) ([]byte, error)) ([]byte, error) {
	protected, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return nil, fmt.Errorf("凭证密文格式无效: %w", err)
	}
	decrypted, err := decrypt(protected)
	if err != nil {
		return nil, fmt.Errorf("解密凭证: %w", err)
	}
	return decrypted, nil
}
