package camoufoxnative

import "testing"

// TestFirefoxPreferencesLeakGuards 验证默认关闭 WebRTC 与 IPv6，并保留代理远程 DNS
func TestFirefoxPreferencesLeakGuards(t *testing.T) {
	prefs, err := firefoxPreferences("", "")
	if err != nil {
		t.Fatal(err)
	}
	if prefs["media.peerconnection.enabled"] != false {
		t.Fatal("WebRTC 没有被关闭")
	}
	if prefs["media.peerconnection.ice.no_host"] != true {
		t.Fatal("WebRTC 没有禁用本机候选地址")
	}
	if prefs["network.dns.disableIPv6"] != true {
		t.Fatal("IPv6 没有被关闭")
	}
	prefs, err = firefoxPreferences("socks5://127.0.0.1:1080", "")
	if err != nil {
		t.Fatal(err)
	}
	if prefs["network.proxy.socks_remote_dns"] != true {
		t.Fatal("SOCKS5 没有启用远程 DNS")
	}
}
