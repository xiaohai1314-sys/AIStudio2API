package api

import "regexp"

// redactedValue 替换正文中的凭据值，保留字段名便于排查
const redactedValue = "[redacted]"

var sensitiveJSONPattern = regexp.MustCompile(`(?i)"(api[_-]?key|apikey|x-goog-api-key|authorization|access[_-]?token|refresh[_-]?token|id[_-]?token|token|password|secret|client[_-]?secret|cookie|set-cookie)"(\s*:\s*)"(?:[^"\\]|\\.)*"`)

var sensitiveQueryPattern = regexp.MustCompile(`(?i)([?&](?:key|api_key|access_token|refresh_token|token|password)=)[^&\s"'<>]+`)

// redactBody 脱敏正文中的 API key、令牌与密码，避免请求正文日志保存可用凭据
func redactBody(text string) string {
	if text == "" {
		return text
	}
	text = sensitiveJSONPattern.ReplaceAllString(text, `"$1"$2"`+redactedValue+`"`)
	return sensitiveQueryPattern.ReplaceAllString(text, "$1"+redactedValue)
}
