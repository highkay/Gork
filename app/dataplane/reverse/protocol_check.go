package reverse

import (
	"context"
	"fmt"
	"strings"
	"time"

	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

var DefaultProtocolCheckTargets = []string{"models", "chat", "image", "video", "voice"}

type ProtocolCheckResult struct {
	Target    string `json:"target"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	ErrorType string `json:"error_type,omitempty"`
	RequestID string `json:"request_id"`
	CheckedAt string `json:"checked_at"`
}

type ProtocolChecker interface {
	CheckProtocolTarget(context.Context, string) ProtocolCheckResult
}

type EndpointProtocolChecker struct {
	Endpoints reverseruntime.EndpointTable
	Now       func() time.Time
}

func RunProtocolCheck(ctx context.Context, targets []string, checker ProtocolChecker) []ProtocolCheckResult {
	if len(targets) == 0 {
		targets = DefaultProtocolCheckTargets
	}
	if checker == nil {
		checker = EndpointProtocolChecker{Endpoints: reverseruntime.GlobalEndpointTable()}
	}
	results := make([]ProtocolCheckResult, 0, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		results = append(results, checker.CheckProtocolTarget(ctx, target))
	}
	return results
}

func (c EndpointProtocolChecker) CheckProtocolTarget(_ context.Context, target string) ProtocolCheckResult {
	start := c.now()
	endpointName := protocolCheckEndpointName(target)
	endpoint := c.Endpoints.Resolve(endpointName)
	result := ProtocolCheckResult{
		Target:    target,
		Status:    "ok",
		LatencyMS: c.now().Sub(start).Milliseconds(),
		RequestID: fmt.Sprintf("protocol-%d", start.UnixNano()),
		CheckedAt: start.UTC().Format(time.RFC3339),
	}
	if endpoint == "" {
		result.Status = "error"
		result.ErrorType = "missing_endpoint"
	}
	return result
}

func (c EndpointProtocolChecker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func protocolCheckEndpointName(target string) string {
	switch target {
	case "models":
		return "console_responses"
	case "chat":
		return "chat"
	case "image":
		return "ws_imagine"
	case "video":
		return "media_post"
	case "voice":
		return "livekit_tokens"
	default:
		return ""
	}
}
