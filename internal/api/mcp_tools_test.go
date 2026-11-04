package api

import (
	"context"
	"strings"
	"testing"

	"github.com/Mag1cFall/AIStudio2API/internal/aistudio"
)

// TestConnectMCPRejectsPrivateTargets 验证解析到本机、私网或链路本地的 MCP 服务器被拒绝
func TestConnectMCPRejectsPrivateTargets(t *testing.T) {
	server := &server{}
	for _, target := range []string{
		"http://127.0.0.1:9/mcp",
		"http://10.0.0.1:9/mcp",
		"http://169.254.169.254/latest/meta-data",
	} {
		_, err := server.connectMCP(context.Background(), aistudio.MCPServer{Name: "local", URL: target}, "tools/list")
		if err == nil || !strings.Contains(err.Error(), "non-public") {
			t.Fatalf("%s: err = %v", target, err)
		}
	}
}
