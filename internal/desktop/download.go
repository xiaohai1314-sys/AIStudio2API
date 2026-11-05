package desktop

import (
	"fmt"
	"io"
)

func copyWithLimit(dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	if maxBytes <= 0 || maxBytes == int64(^uint64(0)>>1) {
		return 0, fmt.Errorf("download byte limit must be positive and finite")
	}
	written, err := io.Copy(dst, io.LimitReader(src, maxBytes+1))
	if err != nil {
		return written, err
	}
	if written > maxBytes {
		return written, fmt.Errorf("download exceeds %d bytes", maxBytes)
	}
	return written, nil
}
