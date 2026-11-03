//go:build windows

package securefile

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	crypt32                = windows.NewLazySystemDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
)

// cryptProtectUIForbidden 禁止 DPAPI 弹出任何界面
const cryptProtectUIForbidden = 0x1

// dataBlob 对应 Win32 的 DATA_BLOB
type dataBlob struct {
	size uint32
	data *byte
}

func protect(data []byte) ([]byte, error) {
	return dpapiCall(procCryptProtectData, data)
}

func unprotect(data []byte) ([]byte, error) {
	return dpapiCall(procCryptUnprotectData, data)
}

// dpapiCall 调用 CryptProtectData 或 CryptUnprotectData，并在释放前复制输出
func dpapiCall(procedure *windows.LazyProc, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	input := dataBlob{size: uint32(len(data)), data: &data[0]}
	var output dataBlob
	result, _, callErr := procedure.Call(
		uintptr(unsafe.Pointer(&input)),
		0, 0, 0, 0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		if callErr == nil || errors.Is(callErr, windows.ERROR_SUCCESS) {
			callErr = windows.GetLastError()
		}
		return nil, fmt.Errorf("DPAPI 调用失败: %w", callErr)
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.data))))
	return append([]byte(nil), unsafe.Slice(output.data, int(output.size))...), nil
}
