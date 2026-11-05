package camoufoxnative

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// AccountFingerprint 返回账户固定 Camoufox 指纹配置，不存在时按当前版本生成并保存
func AccountFingerprint(options Options) (map[string]any, error) {
	return loadAccountCamoufoxConfig(options.StorageStatePath, camoufoxFirefoxMajor, options.Locale, options.Timezone)
}

// AccountFingerprintDigest 返回账户固定指纹配置的 SHA-256 摘要
// 不做 (路径, 大小, 修改时间) 缓存：loadAccountCamoufoxConfig 本就每次读文件，
// 缓存只省一次小 JSON 的序列化与哈希，却会在同尺寸同修改时间的内容替换时返回过期摘要
func AccountFingerprintDigest(options Options) (string, error) {
	config, err := AccountFingerprint(options)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("编码账户 Camoufox 指纹: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
