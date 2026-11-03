package camoufoxnative

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// fingerprintDigestEntry 缓存指纹文件未被改写时的摘要
type fingerprintDigestEntry struct {
	modTime time.Time
	size    int64
	digest  string
}

var fingerprintDigests sync.Map

// AccountFingerprint 返回账户固定 Camoufox 指纹配置，不存在时按当前版本生成并保存
func AccountFingerprint(options Options) (map[string]any, error) {
	return loadAccountCamoufoxConfig(options.StorageStatePath, camoufoxFirefoxMajor, options.Locale, options.Timezone)
}

// AccountFingerprintDigest 返回账户固定指纹配置的 SHA-256 摘要，文件未变化时复用上次结果
func AccountFingerprintDigest(options Options) (string, error) {
	path := filepath.Join(filepath.Dir(options.StorageStatePath), "camoufox-fingerprint.json")
	if info, err := os.Stat(path); err == nil {
		if cached, ok := fingerprintDigests.Load(path); ok {
			entry := cached.(fingerprintDigestEntry)
			if entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
				return entry.digest, nil
			}
		}
	}
	config, err := AccountFingerprint(options)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("编码账户 Camoufox 指纹: %w", err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if info, err := os.Stat(path); err == nil {
		fingerprintDigests.Store(path, fingerprintDigestEntry{modTime: info.ModTime(), size: info.Size(), digest: digest})
	}
	return digest, nil
}
