package api

import (
	"net/url"
	"testing"
	"time"
)

// TestUsageCursorRoundTrip 验证分页位置编码后能原样解析，非法输入返回错误
func TestUsageCursorRoundTrip(t *testing.T) {
	cursor := UsageCursor{Time: time.Date(2026, 3, 5, 12, 0, 0, 123_000_000, time.UTC), ID: "req-1"}
	encoded := EncodeUsageCursor(cursor)
	decoded, err := decodeUsageCursor(encoded)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !decoded.Time.Equal(cursor.Time) || decoded.ID != cursor.ID {
		t.Fatalf("往返结果 = %+v, want %+v", decoded, cursor)
	}
	for _, invalid := range []string{"!!!", "MTIzNDU2", "notanumber|id", "123456|"} {
		if _, err := decodeUsageCursor(invalid); err == nil {
			t.Fatalf("非法分页位置 %q 应当报错", invalid)
		}
	}
}

// TestAutoUsageBucket 验证分桶时长按时间范围长度选择
func TestAutoUsageBucket(t *testing.T) {
	cases := []struct {
		span time.Duration
		want time.Duration
	}{
		{span: time.Minute, want: time.Minute},
		{span: 2 * time.Hour, want: time.Minute},
		{span: 2*time.Hour + time.Second, want: 5 * time.Minute},
		{span: 12 * time.Hour, want: 5 * time.Minute},
		{span: 12*time.Hour + time.Second, want: time.Hour},
		{span: 8 * 24 * time.Hour, want: time.Hour},
		{span: 8*24*time.Hour + time.Second, want: 24 * time.Hour},
	}
	for _, test := range cases {
		if got := autoUsageBucket(test.span); got != test.want {
			t.Fatalf("autoUsageBucket(%s) = %s, want %s", test.span, got, test.want)
		}
	}
}

// TestParseUsageRange 验证时间范围解析的边界校验
func TestParseUsageRange(t *testing.T) {
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)
	gotFrom, gotTo, err := parseUsageRange(url.Values{
		"from": {from.Format(time.RFC3339)}, "to": {to.Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("合法范围解析失败: %v", err)
	}
	if !gotFrom.Equal(from) || !gotTo.Equal(to) {
		t.Fatalf("解析结果 = %s ~ %s", gotFrom, gotTo)
	}
	cases := map[string]url.Values{
		"缺少 from":     {"to": []string{to.Format(time.RFC3339)}},
		"from 格式错误":   {"from": []string{"2026-03-01"}, "to": []string{to.Format(time.RFC3339)}},
		"from 不早于 to": {"from": []string{to.Format(time.RFC3339)}, "to": []string{to.Format(time.RFC3339)}},
		"范围超过 93 天":   {"from": []string{from.Format(time.RFC3339)}, "to": []string{from.Add(usageMaxSpan + time.Hour).Format(time.RFC3339)}},
	}
	for name, values := range cases {
		if _, _, err := parseUsageRange(values); err == nil {
			t.Fatalf("%s 应当报错", name)
		}
	}
}

// TestParseUsageFilters 验证筛选值按逗号拆分、去空白并去重
func TestParseUsageFilters(t *testing.T) {
	filters := parseUsageFilters(map[string][]string{
		"model":   {"gemini-2.5-pro, gemini-2.5-flash", "gemini-2.5-pro", " , "},
		"account": {"user@example.com"},
		"unknown": {"ignored"},
	})
	models := filters["model"]
	if len(models) != 2 || models[0] != "gemini-2.5-pro" || models[1] != "gemini-2.5-flash" {
		t.Fatalf("模型筛选 = %v", models)
	}
	if accounts := filters["account"]; len(accounts) != 1 || accounts[0] != "user@example.com" {
		t.Fatalf("账户筛选 = %v", accounts)
	}
	if _, exists := filters["unknown"]; exists {
		t.Fatal("未知维度不应出现在筛选结果中")
	}
}
