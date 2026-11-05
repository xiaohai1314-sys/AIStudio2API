package aistudio

import (
	"strings"
	"testing"
)

func TestReadResponseBodyRejectsOversize(t *testing.T) {
	_, err := ReadResponseBody(strings.NewReader("12345"), 4)
	if err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("ReadResponseBody error = %v, want oversize error", err)
	}
}

func TestReadResponseBodyAcceptsLimit(t *testing.T) {
	body, err := ReadResponseBody(strings.NewReader("1234"), 4)
	if err != nil || string(body) != "1234" {
		t.Fatalf("ReadResponseBody = %q, %v", body, err)
	}
}

func TestDrainResponseBodyRejectsOversize(t *testing.T) {
	if _, err := DrainResponseBody(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("DrainResponseBody accepted oversized response")
	}
}
