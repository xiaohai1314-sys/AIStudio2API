package api

import (
	"context"
	"strings"
	"testing"
)

func TestInteractionWebhookRejectsNonPublicTargets(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1:8080/hooks",
		"http://10.0.0.1/hooks",
		"http://169.254.169.254/latest/meta-data",
		"http://100.64.0.1/hooks",
	} {
		t.Run(target, func(t *testing.T) {
			err := (interactionWebhookConfig{URIs: []string{target}}).validate(context.Background(), true)
			if err == nil || !strings.Contains(err.Error(), "public http or https") {
				t.Fatalf("validate(%q) = %v, want public-target rejection", target, err)
			}
		})
	}
}

func TestInteractionWebhookLimitsTargets(t *testing.T) {
	urls := make([]string, maxInteractionWebhookURIs+1)
	for index := range urls {
		urls[index] = "https://example.com/hook"
	}
	err := (interactionWebhookConfig{URIs: urls}).validate(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("validate() = %v, want target limit error", err)
	}
}
