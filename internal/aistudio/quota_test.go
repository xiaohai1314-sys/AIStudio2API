package aistudio

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// TestQuotaCooldownForError 验证上游额度错误被归入正确的冷却类型与时长
func TestQuotaCooldownForError(t *testing.T) {
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	tooMany := func(metadata map[string]string, message string) error {
		return &RPCError{StatusCode: http.StatusTooManyRequests, Metadata: metadata, Message: message}
	}
	cases := []struct {
		name     string
		err      error
		wantOK   bool
		wantKind string
		wantEnd  time.Time
		global   bool
		escalate bool
	}{
		{name: "非额度错误", err: errors.New("boom")},
		{name: "状态码不是 429", err: &RPCError{StatusCode: http.StatusInternalServerError}},
		{
			name: "每日额度", err: tooMany(map[string]string{"quota_unit": "1/day/{project}"}, "quota exceeded"),
			wantOK: true, wantKind: "每日限额", wantEnd: time.Date(2026, 3, 6, 8, 0, 0, 0, time.UTC),
		},
		{
			name: "分钟额度", err: tooMany(map[string]string{"quota_unit": "1/min/{project}"}, "quota exceeded"),
			wantOK: true, wantKind: "分钟限额", wantEnd: now.Add(time.Minute),
		},
		{
			name: "全局额度", err: tooMany(map[string]string{"quota_metric": "requests_global"}, "quota exceeded"),
			wantOK: true, wantKind: "短期限额", wantEnd: now.Add(time.Minute), global: true,
		},
		{
			name: "按模型额度不是全局", err: tooMany(map[string]string{"quota_metric": "requests_global_per_model"}, "quota exceeded"),
			wantOK: true, wantKind: "短期限额", wantEnd: now.Add(time.Minute),
		},
		{
			name: "额度上限为零", err: tooMany(nil, "quota exceeded: limit: 0, limit: 0"),
			wantOK: true, wantKind: "无配额", wantEnd: now.Add(time.Minute),
		},
		{
			name:   "套餐限额写明恢复时长",
			err:    tooMany(nil, "You have reached your usage limit. Resets in 2 hours, 30 minutes."),
			wantOK: true, wantKind: PlanLimitKind, wantEnd: now.Add(2*time.Hour + 30*time.Minute),
		},
		{
			name: "套餐限额未写恢复时长", err: tooMany(nil, "You have reached your usage limit."),
			wantOK: true, wantKind: PlanLimitKind, wantEnd: now.Add(time.Minute), escalate: true,
		},
		{
			name:   "上游给出重试延迟",
			err:    &RPCError{StatusCode: http.StatusTooManyRequests, RetryDelay: 90 * time.Second, Message: "slow down"},
			wantOK: true, wantKind: "短期限额", wantEnd: now.Add(90 * time.Second),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cooldown, ok := QuotaCooldownForError(test.err, now)
			if ok != test.wantOK {
				t.Fatalf("识别结果 = %t, want %t", ok, test.wantOK)
			}
			if !ok {
				return
			}
			if cooldown.Kind != test.wantKind {
				t.Fatalf("类型 = %q, want %q", cooldown.Kind, test.wantKind)
			}
			if !cooldown.Until.Equal(test.wantEnd) {
				t.Fatalf("结束时间 = %s, want %s", cooldown.Until, test.wantEnd)
			}
			if cooldown.Global != test.global {
				t.Fatalf("全局标记 = %t, want %t", cooldown.Global, test.global)
			}
			if cooldown.Escalate != test.escalate {
				t.Fatalf("递增标记 = %t, want %t", cooldown.Escalate, test.escalate)
			}
		})
	}
}

// TestNextQuotaDay 验证每日额度按太平洋时间零点重置
func TestNextQuotaDay(t *testing.T) {
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{now: time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC), want: time.Date(2026, 3, 6, 8, 0, 0, 0, time.UTC)},
		{now: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), want: time.Date(2026, 7, 2, 7, 0, 0, 0, time.UTC)},
	}
	for _, test := range cases {
		if got := nextQuotaDay(test.now); !got.Equal(test.want) {
			t.Fatalf("nextQuotaDay(%s) = %s, want %s", test.now, got, test.want)
		}
	}
}

// TestMinuteQuotaReset 验证分钟额度按上游窗口开始时间推算恢复时刻，异常输入回退到一分钟
func TestMinuteQuotaReset(t *testing.T) {
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	if got := minuteQuotaReset("", now); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("缺少窗口时间时 = %s, want %s", got, now.Add(time.Minute))
	}
	window := now.Add(-30 * time.Second).Unix()
	if got := minuteQuotaReset(strconv.FormatInt(window, 10), now); !got.Equal(now.Add(30 * time.Second)) {
		t.Fatalf("按窗口时间 = %s, want %s", got, now.Add(30*time.Second))
	}
	if got := minuteQuotaReset(strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10), now); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("过期窗口 = %s, want %s", got, now.Add(time.Minute))
	}
}

// TestPlanLimitCooldownEscalates 验证套餐限额冷却按连续次数递增，上一次仍生效时沿用
func TestPlanLimitCooldownEscalates(t *testing.T) {
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	first := planLimitCooldown(CooldownState{}, false, now, "套餐限额")
	if first.Level != 1 || !first.Until.Equal(now.Add(time.Minute)) {
		t.Fatalf("首次冷却 = %+v", first)
	}
	second := planLimitCooldown(first, true, now.Add(2*time.Minute), "套餐限额")
	if second.Level != 2 || !second.Until.Equal(now.Add(2*time.Minute+5*time.Minute)) {
		t.Fatalf("第二次冷却 = %+v", second)
	}
	active := planLimitCooldown(second, true, now.Add(3*time.Minute), "套餐限额")
	if active != second {
		t.Fatalf("冷却未结束时应沿用上一次: %+v", active)
	}
}
