// Package paniclog 记录后台 goroutine 的 panic，避免单个 goroutine 终止整个进程
package paniclog

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// Go 在独立 goroutine 中执行 fn，panic 时记录 label、值与堆栈而不终止进程
func Go(label string, fn func()) {
	go func() {
		defer Recover(label)
		fn()
	}()
}

// Recover 在 defer 中记录当前 goroutine 的 panic
func Recover(label string) {
	if recovered := recover(); recovered != nil {
		slog.Error("后台 goroutine panic", "label", label, "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
	}
}
