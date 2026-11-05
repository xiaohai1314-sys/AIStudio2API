package api

import (
	"bytes"
	"strings"
	"testing"
)

func TestCopyMediaStreamRejectsOversizeWithoutWritingOverflow(t *testing.T) {
	var destination bytes.Buffer
	written, err := copyMediaStream(&destination, strings.NewReader("12345"), 4)
	if err == nil || written != 4 || destination.String() != "1234" {
		t.Fatalf("copyMediaStream = (%d, %v, %q), want 4 bytes and an oversize error", written, err, destination.String())
	}
}

func TestCopyMediaStreamAcceptsExactLimit(t *testing.T) {
	var destination bytes.Buffer
	written, err := copyMediaStream(&destination, strings.NewReader("1234"), 4)
	if err != nil || written != 4 || destination.String() != "1234" {
		t.Fatalf("copyMediaStream = (%d, %v, %q), want 4 bytes without error", written, err, destination.String())
	}
}
