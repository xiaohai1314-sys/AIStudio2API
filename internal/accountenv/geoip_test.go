package accountenv

import (
	"net/netip"
	"os"
	"testing"
)

// TestGeoIPCountry 用本机下载的数据库验证国家查询；未设置 GEOIP_TEST_DB 时跳过
func TestGeoIPCountry(t *testing.T) {
	path := os.Getenv("GEOIP_TEST_DB")
	if path == "" {
		t.Skip("未设置 GEOIP_TEST_DB")
	}
	geoip, err := OpenGeoIP(path)
	if err != nil {
		t.Fatal(err)
	}
	defer geoip.Close()
	cases := map[string]string{
		"8.8.8.8":         "US",
		"114.114.114.114": "CN",
	}
	for address, want := range cases {
		country, err := geoip.Country(netip.MustParseAddr(address))
		if err != nil {
			t.Fatal(err)
		}
		if country != want {
			t.Fatalf("%s 国家 = %q，期望 %q", address, country, want)
		}
	}
}
