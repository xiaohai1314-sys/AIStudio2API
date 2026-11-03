package app

import "testing"

func TestGenerateRetryAttemptLimit(t *testing.T) {
	cases := []struct {
		base int
		want int
	}{
		{base: 1, want: 2},
		{base: 2, want: 4},
		{base: 3, want: 6},
		{base: 4, want: 8},
		{base: 5, want: 8},
		{base: 20, want: 20},
	}
	for _, test := range cases {
		retry := &generateRetry{maxAttempts: test.base, baseAttempts: test.base}
		if got := retry.attemptLimit(); got != test.want {
			t.Fatalf("baseAttempts=%d 的上限应为 %d，得到 %d", test.base, test.want, got)
		}
	}
}

func TestGenerateRetryExtendStopsAtLimit(t *testing.T) {
	retry := &generateRetry{maxAttempts: 1, baseAttempts: 1}
	for range 10 {
		retry.extend()
	}
	if retry.maxAttempts != 2 {
		t.Fatalf("单账户请求的尝试次数应停在 2，得到 %d", retry.maxAttempts)
	}
}

func TestGenerateRetryExtendKeepsLargeBudget(t *testing.T) {
	retry := &generateRetry{maxAttempts: 20, baseAttempts: 20}
	retry.extend()
	if retry.maxAttempts != 20 {
		t.Fatalf("初始预算超过硬上限时不应扩容，得到 %d", retry.maxAttempts)
	}
}
