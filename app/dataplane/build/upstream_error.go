package build

import (
	"errors"
	"fmt"
)

// UpstreamError 表示 Build 上游 HTTP 非成功响应。
type UpstreamError struct {
	Status int
	Body   string
	Op     string
}

func (e *UpstreamError) Error() string {
	if e == nil {
		return ""
	}
	op := e.Op
	if op == "" {
		op = "request"
	}
	if e.Body != "" {
		return fmt.Sprintf("build upstream %s status=%d body=%s", op, e.Status, e.Body)
	}
	return fmt.Sprintf("build upstream %s status=%d", op, e.Status)
}

// IsUnauthorized 判断是否为 401。
func IsUnauthorized(err error) bool {
	var ue *UpstreamError
	return errors.As(err, &ue) && ue.Status == 401
}

// IsRateLimited 判断是否为 429。
func IsRateLimited(err error) bool {
	var ue *UpstreamError
	return errors.As(err, &ue) && ue.Status == 429
}
