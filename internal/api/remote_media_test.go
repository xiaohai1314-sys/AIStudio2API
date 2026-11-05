package api

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestPublicRemoteAddress(t *testing.T) {
	for _, value := range []string{
		"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "192.0.2.1", "240.0.0.1", "2001:db8::1",
	} {
		if publicRemoteAddress(netip.MustParseAddr(value)) {
			t.Fatalf("non-public address %s was accepted", value)
		}
	}
	if !publicRemoteAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address was rejected")
	}
}

func TestRemoteMediaRequestBudget(t *testing.T) {
	b := &remoteMediaRequestBudget{}
	first, err := b.reserve(context.Background(), remoteMediaMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer first.release()
	second, err := b.reserve(context.Background(), remoteMediaMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer second.release()
	if _, err := b.reserve(context.Background(), 1); err == nil {
		t.Fatal("request budget accepted a third reservation")
	}
	first.release()
	if b.used != remoteMediaMaxBytes {
		t.Fatalf("remaining budget=%d", b.used)
	}
}

func TestRemoteMediaBudgetReleaseAndResize(t *testing.T) {
	remoteMediaGlobalBudget.Lock()
	before := remoteMediaGlobalBudget.used
	remoteMediaGlobalBudget.Unlock()

	budget := &remoteMediaRequestBudget{}
	reservation, err := budget.reserve(context.Background(), remoteMediaMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.resize(1 << 20); err != nil {
		t.Fatal(err)
	}
	budget.release()
	budget.release()
	if budget.used != 0 {
		t.Fatalf("request budget after release = %d, want 0", budget.used)
	}
	remoteMediaGlobalBudget.Lock()
	got := remoteMediaGlobalBudget.used
	remoteMediaGlobalBudget.Unlock()
	if got != before {
		t.Fatalf("global budget after release = %d, want %d", got, before)
	}
}

func TestRemoteMediaGlobalBudgetHonorsCancellation(t *testing.T) {
	var budgets []*remoteMediaRequestBudget
	for range int(remoteMediaGlobalBytes / remoteMediaMaxBytes) {
		budget := &remoteMediaRequestBudget{}
		if _, err := budget.reserve(context.Background(), remoteMediaMaxBytes); err != nil {
			for _, held := range budgets {
				held.release()
			}
			t.Fatal(err)
		}
		budgets = append(budgets, budget)
	}
	t.Cleanup(func() {
		for _, held := range budgets {
			held.release()
		}
	})

	waiting := &remoteMediaRequestBudget{}
	ctx, cancel := context.WithCancel(context.Background())
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		_, err := waiting.reserve(ctx, 1)
		done <- err
	}()
	<-started
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	for {
		waiting.mu.Lock()
		queued := waiting.used == 1
		waiting.mu.Unlock()
		if queued {
			break
		}
		select {
		case <-timeout.C:
			cancel()
			<-done
			t.Fatal("reserve did not enter global budget wait")
		case <-ticker.C:
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("reserve error = %v, want context.Canceled", err)
	}
	if waiting.used != 0 {
		t.Fatalf("cancelled request retained %d bytes", waiting.used)
	}
}

func TestImageEditMaskLimits(t *testing.T) {
	if _, err := imageEditMask(make([]byte, remoteMediaMaxBytes+1)); err == nil {
		t.Fatal("oversized mask accepted")
	}
}

func TestPathUnescapeLimited(t *testing.T) {
	decoded, err := pathUnescapeLimited("%41%42", 2)
	if err != nil || decoded != "AB" {
		t.Fatalf("pathUnescapeLimited = %q, %v", decoded, err)
	}
	if _, err := pathUnescapeLimited(strings.Repeat("%41", 3), 2); err == nil {
		t.Fatal("oversized escaped data URL was accepted")
	}
}

func TestImageEditMaskRejectsOversizedDimensions(t *testing.T) {
	if _, err := imageEditMask(pngHeader(10_001, 1)); err == nil {
		t.Fatal("mask with oversized width was accepted")
	}
	if _, err := imageEditMask(pngHeader(6_400, 6_400)); err == nil {
		t.Fatal("mask above pixel limit was accepted")
	}
}

func pngHeader(width, height uint32) []byte {
	data := make([]byte, 8+4+4+13+4)
	copy(data, []byte("\x89PNG\r\n\x1a\n"))
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	data[24], data[25] = 8, 6
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}
