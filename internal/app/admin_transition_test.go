package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
	"github.com/Mag1cFall/AIStudio2API/internal/api"
)

func TestSetAccountEnabledPanicFinishesTransitionAndPublishesSnapshot(t *testing.T) {
	ctx := context.Background()
	account := &aistudio.Account{
		ID:         "user@example.com",
		ConfigPath: filepath.Join(t.TempDir(), "account.json"),
		Config: aistudio.AccountConfig{
			Label: "user@example.com", Enabled: true, Locale: "en-US", Timezone: "UTC",
		},
		State: aistudio.AccountReady,
	}
	pool := aistudio.NewAccountPool([]*aistudio.Account{account}, 1)
	requests := &requestRegistry{
		active:      make(map[string]trackedRequest),
		subscribers: make(map[*eventSubscriber]struct{}),
	}
	subscriber := newEventSubscriber(ctx)
	requests.subscribers[subscriber] = struct{}{}
	service := &trackedService{lifecycle: ctx, pool: pool, requests: requests}
	admin := &runtimeAdmin{lifecycle: ctx, pool: pool, service: service, requests: requests}

	if _, err := admin.setAccountEnabled(account.ID, true); err != nil {
		t.Fatalf("setAccountEnabled returned an error: %v", err)
	}
	// A missing proxy pool intentionally makes finishEnable panic after the
	// transition starts; the background panic must not leave stale state behind.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		statuses := pool.Status()
		admin.transitionMu.Lock()
		_, transitionTracked := admin.transitions[account.ID]
		admin.transitionMu.Unlock()

		var finalAccounts []api.AdminAccount
		var hasStatusSnapshot bool
		subscriber.mu.Lock()
		for _, event := range subscriber.pending {
			switch event.Type {
			case "status":
				hasStatusSnapshot = true
			case "accounts":
				payload, ok := event.Data.(map[string]any)
				if !ok {
					continue
				}
				finalAccounts, _ = payload["accounts"].([]api.AdminAccount)
			}
		}
		subscriber.mu.Unlock()

		if len(statuses) == 1 && statuses[0].Transition == "" && !transitionTracked &&
			hasStatusSnapshot && len(finalAccounts) == 1 && finalAccounts[0].Transition == "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("panic left the account transition unfinished or failed to publish the final snapshot")
}
