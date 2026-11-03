package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// interactionWebhookClient 发送 webhook 事件，接收端需在数秒内应答；重定向也不能进入内网。
var interactionWebhookClient = &http.Client{
	Transport: publicOutboundTransport,
	Timeout:   10 * time.Second,
	CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return checkRemoteMediaHost(request.Context(), request.URL.Hostname())
	},
}

// interactionWebhookWindow 是 webhook 事件投递失败后继续重试的时长
const (
	interactionWebhookWindow  = 24 * time.Hour
	maxInteractionWebhookURIs = 8
)

// interactionWebhookConfig 是请求级 webhook 的地址与随事件返回的用户元数据
type interactionWebhookConfig struct {
	URIs         []string        `json:"uris"`
	UserMetadata json.RawMessage `json:"user_metadata"`
}

// interactionWebhookEvent 是按官方字段顺序编码的 webhook 事件信封
type interactionWebhookEvent struct {
	Type         string                 `json:"type"`
	Version      string                 `json:"version"`
	Timestamp    string                 `json:"timestamp"`
	Data         interactionWebhookData `json:"data"`
	UserMetadata json.RawMessage        `json:"user_metadata,omitempty"`
}

// interactionWebhookData 是事件指向的交互 ID 与失败时的错误
type interactionWebhookData struct {
	ID           string `json:"id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// interactionWebhookEvents 是交互终态对应的 webhook 事件类型
var interactionWebhookEvents = map[string]string{
	"completed": "interaction.completed", "incomplete": "interaction.completed", "requires_action": "interaction.requires_action",
	"failed": "interaction.failed", "cancelled": "interaction.cancelled",
}

// validate 要求请求为后台交互，限制 webhook 数量，并拒绝内网回调目标。
func (config interactionWebhookConfig) validate(ctx context.Context, background bool) error {
	if !background {
		return fmt.Errorf("webhook_config requires background to be true")
	}
	if len(config.URIs) > maxInteractionWebhookURIs {
		return fmt.Errorf("webhook_config.uris allows at most %d URLs", maxInteractionWebhookURIs)
	}
	for _, uri := range config.URIs {
		if _, err := validatePublicHTTPURL(ctx, uri); err != nil {
			return fmt.Errorf("webhook_config.uris must be public http or https URLs: %q: %w", uri, err)
		}
	}
	return nil
}

// sendInteractionWebhook 向每个地址发送交互终态事件，failure 为后台请求写出的 Gemini 错误
func sendInteractionWebhook(config interactionWebhookConfig, id, status string, failure []byte) {
	event := interactionWebhookEvent{Type: interactionWebhookEvents[status], Version: "v1", Timestamp: time.Now().UTC().Format(time.RFC3339), Data: interactionWebhookData{ID: id}}
	var written struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if status == "failed" && json.Unmarshal(failure, &written) == nil {
		event.Data.ErrorCode, event.Data.ErrorMessage = written.Error.Status, written.Error.Message
	}
	if rawJSONConfigured(config.UserMetadata) {
		event.UserMetadata = config.UserMetadata
	}
	body, _ := json.Marshal(event)
	messageID := newID("msg")
	for _, uri := range config.URIs {
		go deliverInteractionWebhook(uri, messageID, body)
	}
}

// deliverInteractionWebhook 以 webhook-id 与 webhook-timestamp 请求头 POST 事件，非 2xx 时指数退避重试，直到成功或超过重试时长
func deliverInteractionWebhook(uri, messageID string, body []byte) {
	deadline := time.Now().Add(interactionWebhookWindow)
	for delay := time.Second; ; delay = min(delay*2, time.Hour) {
		request, _ := http.NewRequest(http.MethodPost, uri, bytes.NewReader(body))
		if err := checkRemoteMediaHost(context.Background(), request.URL.Hostname()); err != nil {
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("webhook-id", messageID)
		request.Header.Set("webhook-timestamp", strconv.FormatInt(time.Now().Unix(), 10))
		if response, err := interactionWebhookClient.Do(request); err == nil {
			response.Body.Close()
			if response.StatusCode/100 == 2 {
				return
			}
		}
		if time.Now().Add(delay).After(deadline) {
			return
		}
		time.Sleep(delay)
	}
}
