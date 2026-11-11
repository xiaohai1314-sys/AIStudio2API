package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

func TestPushRenewalExtractsRevocationCodeFromLargeErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(writer, `{"error":{"message":%q,"code":"pairing_renewal_invalid"}}`, strings.Repeat("x", 1<<20))
	}))
	defer server.Close()

	state := renewalState{
		renewal:  remoteRenewal{Origin: server.URL, Account: "account", Token: "token"},
		cookies:  []aistudio.StateCookie{{Name: "session", Value: "cookie"}},
		mintedAt: time.Now(),
	}

	err := pushRenewal(context.Background(), server.Client(), &state)
	if !errors.Is(err, errRenewalRevoked) {
		t.Fatalf("大错误响应中的撤销码未被识别: %v", err)
	}
}
