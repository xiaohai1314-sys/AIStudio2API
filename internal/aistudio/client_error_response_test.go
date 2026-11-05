package aistudio

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestValidateRPCResponseCapsErrorBody(t *testing.T) {
	reader := strings.NewReader(strings.Repeat("x", int(maxErrorResponseBytes+100)))
	response := &RPCResponse{
		StatusCode: http.StatusBadGateway,
		Header:     make(http.Header),
		Body:       io.NopCloser(reader),
	}

	if _, err := validateRPCResponse("GenerateContent", response); err == nil || !strings.Contains(err.Error(), "超过 1 MiB") {
		t.Fatalf("validateRPCResponse error = %v, want 1 MiB limit error", err)
	}
	if remaining := reader.Len(); remaining != 99 {
		t.Fatalf("error body consumed %d bytes, want %d", int(maxErrorResponseBytes+100)-remaining, maxErrorResponseBytes+1)
	}
}
