package aistudio

import (
	"fmt"
	"net/http"
	"testing"
)

// TestDetectChallengeRedirect 验证登录与人机验证跳转被识别为风控
func TestDetectChallengeRedirect(t *testing.T) {
	cases := []string{
		"https://accounts.google.com/signin/oauth?continue=https%3A%2F%2Faistudio.google.com",
		"https://www.google.com/sorry/index?continue=https%3A%2F%2Faistudio.google.com",
	}
	for _, location := range cases {
		header := http.Header{"Location": {location}}
		if challenge := DetectChallenge("GenerateContent", http.StatusFound, header, nil); challenge == nil {
			t.Fatalf("跳转 %s 未被识别为风控", location)
		}
	}
	header := http.Header{"Location": {"https://aistudio.google.com/"}}
	if challenge := DetectChallenge("GenerateContent", http.StatusFound, header, nil); challenge != nil {
		t.Fatalf("正常跳转被误判为风控: %v", challenge)
	}
}

// TestDetectChallengeBody 验证风控页面正文被识别为挑战
func TestDetectChallengeBody(t *testing.T) {
	body := []byte("<html><body>Our systems have detected unusual traffic from your computer network.</body></html>")
	if challenge := DetectChallenge("GenerateContent", http.StatusForbidden, nil, body); challenge == nil {
		t.Fatal("风控页面正文未被识别")
	}
	if challenge := DetectChallenge("GenerateContent", http.StatusOK, nil, []byte("<html>g-recaptcha</html>")); challenge == nil {
		t.Fatal("200 响应中的验证码页面未被识别")
	}
}

// TestDetectChallengeIgnoresNormalErrors 验证普通上游错误不被误判为风控
func TestDetectChallengeIgnoresNormalErrors(t *testing.T) {
	region := []byte(`[null,[7,"Region not supported."]]`)
	if challenge := DetectChallenge("GenerateContent", http.StatusForbidden, nil, region); challenge != nil {
		t.Fatalf("地区限制被误判为风控: %v", challenge)
	}
	if challenge := DetectChallenge("GenerateContent", http.StatusInternalServerError, nil, []byte("unusual traffic")); challenge != nil {
		t.Fatalf("非风控状态码被误判: %v", challenge)
	}
}

// TestChallengeErrorUnwrap 验证风控错误可经错误链识别
func TestChallengeErrorUnwrap(t *testing.T) {
	err := fmt.Errorf("发送请求: %w", &ChallengeError{Method: "GenerateContent", StatusCode: http.StatusForbidden})
	if !IsChallenge(err) {
		t.Fatal("包装后的风控错误未被识别")
	}
	if IsChallenge(fmt.Errorf("普通错误")) {
		t.Fatal("普通错误被识别为风控")
	}
}
