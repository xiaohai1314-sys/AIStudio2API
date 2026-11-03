package api

import (
	"net/netip"
	"testing"
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
