package aistudio

import (
	"errors"
	"net/http"
	"strings"
)

// maxChallengeBody 是风控页检测读取的响应体上限
const maxChallengeBody = 64 << 10

// ErrChallenge 表示上游要求人机验证或重新登录
var ErrChallenge = errors.New("上游要求人机验证")

// challengeMarkers 是登录确认与人机验证页面的特征文本，全部小写
var challengeMarkers = []string{
	"unusual traffic",
	"our systems have detected",
	"not a robot",
	"recaptcha",
	"g-recaptcha",
	"accounts.google.com/sorry",
	"/sorry/index",
	"accounts.google.com/signin",
	"accounts.google.com/v3/signin",
	"verify it's you",
	"verify it is you",
}

// ChallengeError 表示上游返回风控页或登录跳转
type ChallengeError struct {
	Method     string
	StatusCode int
	Location   string
	Reason     string
}

// Error 返回风控错误的可读说明
func (e *ChallengeError) Error() string {
	message := "AI Studio " + e.Method + " 返回 HTTP " + http.StatusText(e.StatusCode)
	if e.StatusCode == 0 {
		message = "AI Studio " + e.Method
	}
	if e.Location != "" {
		message += "，跳转 " + e.Location
	}
	if e.Reason != "" {
		message += "：" + e.Reason
	}
	return message
}

// HTTPStatus 返回风控对应的公开状态码
func (e *ChallengeError) HTTPStatus() int {
	return http.StatusForbidden
}

// ErrorCode 返回风控错误代码
func (e *ChallengeError) ErrorCode() string {
	return "challenge_required"
}

// Unwrap 把风控错误映射为 ErrChallenge
func (e *ChallengeError) Unwrap() error {
	return ErrChallenge
}

// IsChallenge 判断错误链中是否包含风控挑战
func IsChallenge(err error) bool {
	return errors.Is(err, ErrChallenge)
}

// challengeURL 判断重定向目标是否为登录或人机验证页面
func challengeURL(target string) bool {
	target = strings.ToLower(target)
	for _, marker := range challengeMarkers {
		if strings.Contains(target, marker) {
			return true
		}
	}
	return false
}

// DetectChallenge 检查响应是否为风控页或登录跳转，命中时返回 ChallengeError
func DetectChallenge(method string, statusCode int, header http.Header, body []byte) *ChallengeError {
	location := ""
	if header != nil {
		location = strings.TrimSpace(header.Get("Location"))
	}
	if statusCode >= 300 && statusCode < 400 && location != "" && challengeURL(location) {
		return &ChallengeError{
			Method: method, StatusCode: statusCode, Location: location,
			Reason: "上游重定向到登录或人机验证页面",
		}
	}
	if statusCode != http.StatusOK && statusCode != http.StatusForbidden && statusCode != http.StatusTooManyRequests {
		return nil
	}
	if len(body) > maxChallengeBody {
		body = body[:maxChallengeBody]
	}
	lower := strings.ToLower(string(body))
	for _, marker := range challengeMarkers {
		if strings.Contains(lower, marker) {
			return &ChallengeError{
				Method: method, StatusCode: statusCode, Location: location,
				Reason: "上游返回人机验证页面",
			}
		}
	}
	return nil
}
