package openai

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
)

// buildCoolingSetter 可选：写入 cooling_until 的目录能力。
type buildCoolingSetter interface {
	SetStatusUntil(ctx context.Context, id int64, status string, reason string, until time.Time) error
}

// handleBuildUpstreamFailure 处理 Build 非 2xx：账号冷却 + 对客户端脱敏错误。
// 对齐 chenyme #740：402 spending-limit 强制换号，不透传 add-credits 文案。
func handleBuildUpstreamFailure(
	ctx context.Context,
	dir buildAccountDirectory,
	acc buildaccount.Account,
	status int,
	body string,
	op string,
) error {
	if status == http.StatusPaymentRequired || (status == http.StatusForbidden && build.IsSpendingLimitBody(body)) {
		until := build.PaymentCoolingUntil(acc.Billing, time.Now().UTC())
		reason := "payment required / spending-limit"
		if setter, ok := dir.(buildCoolingSetter); ok {
			_ = setter.SetStatusUntil(ctx, acc.ID, buildaccount.StatusCooling, reason, until)
		} else if dir != nil {
			_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusCooling, reason)
		}
		// 对客户端：503 upstream_unavailable，永不暴露升级/充值提示。
		err := platform.NewUpstreamError("No available Build accounts for this request", http.StatusServiceUnavailable, "")
		err.Code = "upstream_unavailable"
		return err
	}
	if status == http.StatusTooManyRequests {
		if dir != nil {
			_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusCooling, "upstream 429")
		}
		return platform.NewRateLimitError("Build account rate limited")
	}
	// 其它错误：保留状态码，正文脱敏截断
	msg := "Build upstream request failed"
	if status > 0 {
		msg = "Build upstream returned " + http.StatusText(status)
		if msg == "Build upstream returned " {
			msg = "Build upstream request failed"
		}
	}
	// 仍返回 platform.UpstreamError 以便 AdaptErrorResponse 识别
	up := platform.NewUpstreamError(msg, status, sanitizeBuildBody(body))
	if op != "" {
		up.Code = "build_" + op
	}
	return up
}

func sanitizeBuildBody(body string) string {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "credit") || strings.Contains(lower, "supergrok") ||
		strings.Contains(lower, "spending") || strings.Contains(lower, "upgrade") {
		return ""
	}
	if len(body) > 512 {
		return body[:512]
	}
	return body
}

// mapBuildUpstreamError 将 build.UpstreamError 转为客户端可识别的 platform 错误。
func mapBuildUpstreamError(ctx context.Context, dir buildAccountDirectory, acc buildaccount.Account, err error) error {
	if err == nil {
		return nil
	}
	var ue *build.UpstreamError
	if !asBuildUpstream(err, &ue) || ue == nil {
		return err
	}
	return handleBuildUpstreamFailure(ctx, dir, acc, ue.Status, ue.Body, ue.Op)
}

func asBuildUpstream(err error, target **build.UpstreamError) bool {
	if err == nil {
		return false
	}
	return errorsAsBuild(err, target)
}

// errorsAsBuild 避免 openai 包与标准 errors 循环；委托 errors.As。
func errorsAsBuild(err error, target any) bool {
	return errorsAs(err, target)
}
