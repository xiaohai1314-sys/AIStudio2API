// Package securefile 用 Windows DPAPI 加密凭证文件，其他平台保持原样
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

var enabled atomic.Bool

// Configure 设置写出凭证文件时是否加密，进程启动时调用一次
func Configure(value bool) {
	enabled.Store(value)
}

// Enabled 返回是否启用凭证加密
func Enabled() bool {
	return enabled.Load()
}

// Encrypt 在启用时用 DPAPI 加密内容并加上标记；未启用时原样返回
func Encrypt(data []byte) ([]byte, error) {
	if !enabled.Load() {
		return data, nil
	}
	protected, err := protect(data)
	if err != nil {
		return nil, fmt.Errorf("加密凭证: %w", err)
	}
	return []byte(envelopePrefix + base64.StdEncoding.EncodeToString(protected)), nil
}

// Decrypt 解密带标记的凭证内容；未加密的文件原样返回，便于从明文平滑迁移
func Decrypt(data []byte) ([]byte, error) {
	encoded, ok := bytes.CutPrefix(data, []byte(envelopePrefix))
	if !ok {
		return data, nil
	}
	protected, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return nil, fmt.Errorf("凭证密文格式无效: %w", err)
	}
	decrypted, err := unprotect(protected)
	if err != nil {
		return nil, fmt.Errorf("解密凭证: %w", err)
	}
	return decrypted, nil
}
