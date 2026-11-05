package desktop

import (
	"bytes"
	"strings"
	"testing"
)

func TestCopyWithLimitRejectsOversizedResponse(t *testing.T) {
	var destination bytes.Buffer
	written, err := copyWithLimit(&destination, strings.NewReader("12345"), 4)
	if err == nil || written != 5 {
		t.Fatalf("copyWithLimit = (%d, %v), want 5 bytes and an oversize error", written, err)
	}
}

func TestCopyWithLimitAcceptsResponseAtLimit(t *testing.T) {
	var destination bytes.Buffer
	written, err := copyWithLimit(&destination, strings.NewReader("1234"), 4)
	if err != nil || written != 4 || destination.String() != "1234" {
		t.Fatalf("copyWithLimit = (%d, %v, %q), want 4 bytes without error", written, err, destination.String())
	}
}
