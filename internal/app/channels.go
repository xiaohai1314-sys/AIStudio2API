package app

import (
	"log/slog"
	"slices"
	"strings"
	"unicode"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
	"github.com/Mag1cFall/AIStudio2API/internal/api"
	"github.com/Mag1cFall/AIStudio2API/internal/config"
)

// upstreamChannels 把配置中的通道名称转换为调度池通道
func upstreamChannels(names []string) []aistudio.Channel {
	channels := make([]aistudio.Channel, 0, len(names))
	for _, name := range names {
		channels = append(channels, aistudio.Channel(name))
	}
	return channels
}

// modelChannels 把 MODEL_CHANNELS 转成模型到通道的映射，配置已在保存与启动时校验
func modelChannels(value string) map[string]aistudio.Channel {
	parsed, _ := config.ParseModelChannels(value)
	channels := make(map[string]aistudio.Channel, len(parsed))
	for model, channel := range parsed {
		channels[model] = aistudio.Channel(channel)
	}
	return channels
}

// apiKeyRateLimits 解析 API_KEY_RATE_LIMITS 为公开接口的每 key 速率上限
func apiKeyRateLimits(keys, bindings string) map[string]api.KeyRateLimit {
	limits, _ := config.ParseAPIKeyRateLimits(bindings, keys)
	result := make(map[string]api.KeyRateLimit, len(limits))
	for key, limit := range limits {
		result[key] = api.KeyRateLimit{PerMinute: limit.PerMinute}
	}
	return result
}

// modelAccounts 把 MODEL_ACCOUNTS 转成模型到可用账户 ID 的映射，配置已在保存与启动时校验
func modelAccounts(value string) map[string][]string {
	accounts, _ := config.ParseModelAccounts(value)
	return accounts
}

// accountOrder 把 ACCOUNT_ORDER 拆成依次排列的账户 ID
func accountOrder(value string) []string {
	return strings.FieldsFunc(value, func(char rune) bool { return char == ',' || unicode.IsSpace(char) })
}

// apiKeyAccounts 解析 API_KEY_ACCOUNTS 为各 API key 可用的账户 ID；active 是管理服务启动时的配置，已从 keys 中移除而管理服务仍接受的 key 沿用 active 中的绑定
func apiKeyAccounts(keys, bindings string, active config.Config) map[string][]string {
	if unbound := config.UnboundAPIKeys(keys, bindings); len(unbound) > 0 {
		// 未列入 API_KEY_ACCOUNTS 的 key 不受账户范围约束，可以访问全部账户与文件。
		// 不回显 key 内容，只提示数量，操作员可在 .env 里比对两个配置项。
		slog.Warn("API_KEY_ACCOUNTS 未覆盖全部 API key", "unbound_keys", len(unbound), "hint", "未列出的 key 可访问全部账户与文件")
	}
	accounts, _ := config.ParseAPIKeyAccounts(bindings, keys)
	previous, _ := config.ParseAPIKeyAccounts(active.APIKeyAccounts, active.ProxyAPIKey)
	for key, ids := range previous {
		if !slices.Contains(config.APIKeys(keys), key) {
			accounts[key] = ids
		}
	}
	return accounts
}
