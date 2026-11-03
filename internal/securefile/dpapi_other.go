//go:build !windows

package securefile

import "errors"

// errUnsupported 表示当前平台没有 DPAPI
var errUnsupported = errors.New("DPAPI 凭证加密仅支持 Windows")

func protect([]byte) ([]byte, error)   { return nil, errUnsupported }
func unprotect([]byte) ([]byte, error) { return nil, errUnsupported }
