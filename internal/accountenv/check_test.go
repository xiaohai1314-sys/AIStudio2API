package accountenv

import "testing"

// TestCheck 验证语言与时区的离线一致性规则
func TestCheck(t *testing.T) {
	cases := []struct {
		name       string
		locale     string
		timezone   string
		wantCodes  []string
		wantZone   string
		wantLocale string
	}{
		{"一致", "en-US", "America/New_York", nil, "", ""},
		{"语言地区与时区不一致", "zh-CN", "America/New_York", []string{"locale_timezone_mismatch"}, "Asia/Shanghai", "en-US"},
		{"时区非法", "en-US", "Asia/Shangai", []string{"invalid_timezone"}, "America/New_York", ""},
		{"语言非法", "english", "America/New_York", []string{"invalid_locale"}, "", ""},
		{"缺少地区", "en", "America/New_York", []string{"locale_missing_region"}, "", "en-US"},
		{"无国家时区", "en-US", "Etc/UTC", nil, "", ""},
		{"多国家时区匹配", "fr-FR", "Europe/Paris", nil, "", ""},
		{"多国家时区不匹配", "ja-JP", "Europe/Paris", []string{"locale_timezone_mismatch"}, "Asia/Tokyo", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			warnings, suggestion := Check(test.locale, test.timezone)
			if len(warnings) != len(test.wantCodes) {
				t.Fatalf("警告 = %+v，期望 %v", warnings, test.wantCodes)
			}
			for index, code := range test.wantCodes {
				if warnings[index].Code != code {
					t.Fatalf("第 %d 条警告 = %q，期望 %q", index, warnings[index].Code, code)
				}
			}
			if suggestion.Timezone != test.wantZone {
				t.Fatalf("建议时区 = %q，期望 %q", suggestion.Timezone, test.wantZone)
			}
			if suggestion.Locale != test.wantLocale {
				t.Fatalf("建议语言 = %q，期望 %q", suggestion.Locale, test.wantLocale)
			}
		})
	}
}

// TestCheckExit 验证出口 IP 国家与语言、时区的比对
func TestCheckExit(t *testing.T) {
	if warnings, _ := CheckExit("en-US", "America/New_York", "US"); len(warnings) != 0 {
		t.Fatalf("一致时出现警告: %+v", warnings)
	}
	warnings, suggestion := CheckExit("zh-CN", "Asia/Shanghai", "US")
	if len(warnings) != 2 || warnings[0].Code != "locale_exit_mismatch" || warnings[1].Code != "timezone_exit_mismatch" {
		t.Fatalf("警告 = %+v", warnings)
	}
	if suggestion.Timezone != "America/New_York" || suggestion.Locale != "en-US" {
		t.Fatalf("建议 = %+v", suggestion)
	}
}

// TestSplitLocale 验证语言地区拆分与规范化
func TestSplitLocale(t *testing.T) {
	cases := []struct {
		input    string
		language string
		region   string
		valid    bool
	}{
		{"en-US", "en", "US", true},
		{"zh_cn", "zh", "CN", true},
		{"pt", "pt", "", true},
		{"en-419", "en", "419", true},
		{"", "", "", false},
		{"english-US", "", "", false},
		{"en-USA", "", "", false},
	}
	for _, test := range cases {
		language, region, valid := SplitLocale(test.input)
		if language != test.language || region != test.region || valid != test.valid {
			t.Fatalf("SplitLocale(%q) = %q, %q, %v", test.input, language, region, valid)
		}
	}
}
