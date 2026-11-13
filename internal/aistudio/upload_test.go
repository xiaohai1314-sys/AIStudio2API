package aistudio

import (
	"errors"
	"net/http"
	"testing"
)

func quotaRPCError() *RPCError {
	return &RPCError{
		Method:     "DriveUpload",
		StatusCode: http.StatusForbidden,
		Message:    `{"error": {"errors": [{"reason": "storageQuotaExceeded", "message": "The user's Drive storage quota has been exceeded."}]}}`,
	}
}

func TestDriveStorageQuotaExceeded(t *testing.T) {
	if !driveStorageQuotaExceeded(quotaRPCError()) {
		t.Fatal("DriveUpload 403 storageQuotaExceeded 应判定为满额")
	}
	if driveStorageQuotaExceeded(&RPCError{Method: "DriveUpload", StatusCode: http.StatusForbidden, Message: "forbidden"}) {
		t.Fatal("不含 storageQuotaExceeded 的 403 不应判定为满额")
	}
	if driveStorageQuotaExceeded(&RPCError{Method: "DriveUpload", StatusCode: http.StatusUnauthorized, Message: `"storageQuotaExceeded"`}) {
		t.Fatal("非 403 的 storageQuotaExceeded 不应判定为满额")
	}
	if driveStorageQuotaExceeded(&RPCError{Method: "GenerateAccessToken", StatusCode: http.StatusForbidden, Message: `"storageQuotaExceeded"`}) {
		t.Fatal("非 DriveUpload 方法的 storageQuotaExceeded 不应判定为满额")
	}
	if driveStorageQuotaExceeded(errors.New("plain error")) {
		t.Fatal("非 RPC 错误不应判定为满额")
	}
}

// TestUploadFailuresAllQuotaExceeded 覆盖 L1：Drive 满额回退只在全部逐文件
// 失败都是满额时才适用，任一非满额失败（认证、绑定等）都必须返回错误。
func TestUploadFailuresAllQuotaExceeded(t *testing.T) {
	authErr := &RPCError{Method: "DriveUpload", StatusCode: http.StatusUnauthorized, Message: "unauthorized"}

	var none uploadFailures
	if none.allQuotaExceeded() {
		t.Fatal("无失败时不应判定为全部满额")
	}

	var allQuota uploadFailures
	allQuota.Add(quotaRPCError())
	allQuota.Add(quotaRPCError())
	if !allQuota.allQuotaExceeded() {
		t.Fatal("全部失败都是满额时应判定为全部满额")
	}

	var mixed uploadFailures
	mixed.Add(quotaRPCError())
	mixed.Add(authErr)
	if mixed.allQuotaExceeded() {
		t.Fatal("混合失败（含认证失败）时不应判定为全部满额")
	}

	var allOther uploadFailures
	allOther.Add(authErr)
	allOther.Add(errors.New("bind failed"))
	if allOther.allQuotaExceeded() {
		t.Fatal("全部失败都不是满额时不应判定为全部满额")
	}
}
