package build

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
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

// IsPaymentRequired 判断是否为 402（额度/付费墙）。
func IsPaymentRequired(err error) bool {
	var ue *UpstreamError
	return errors.As(err, &ue) && ue.Status == http.StatusPaymentRequired
}

// IsAccountScopedFailure 账号级失败，必须换号重试（402/429）。
func IsAccountScopedFailure(err error) bool {
	return IsPaymentRequired(err) || IsRateLimited(err)
}

// 额度恢复暂停窗口（对齐 chenyme #740/#740 后续精炼）。
const (
	FreeQuotaRecoveryPause        = 2 * time.Hour
	PaymentRequiredRecoveryPause  = 20 * time.Hour
	DefaultPaymentCoolingFallback = PaymentRequiredRecoveryPause
)

// PaymentCoolingUntil 计算 402 后账号冷却截止时间。
// 付费账期已知 → period end；否则 20h fallback（避免立刻回到热池）。
func PaymentCoolingUntil(billing Billing, now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if end, ok := billingPeriodEnd(billing); ok && end.After(now) {
		return end
	}
	return now.Add(DefaultPaymentCoolingFallback)
}

func billingPeriodEnd(b Billing) (time.Time, bool) {
	for _, raw := range []string{b.BillingPeriodEnd, b.UsagePeriodEnd} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02"} {
			if t, err := time.Parse(layout, raw); err == nil {
				return t.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

// IsSpendingLimitBody 识别 spending-limit / credits 耗尽类正文。
func IsSpendingLimitBody(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "spending-limit") ||
		strings.Contains(lower, "run out of credits") ||
		strings.Contains(lower, "out of credits") ||
		strings.Contains(lower, "add credits") ||
		strings.Contains(lower, "supergrok")
}
