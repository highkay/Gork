package products

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/logging"
	"github.com/dslzl/gork/app/platform/redact"
)

var consoleStreamPosterFactory = func() protocol.ConsoleStreamPoster {
	return consoleHTTPPoster{}
}

func StreamConsoleChat(ctx context.Context, token string, payload map[string]any, timeoutS float64) ([]protocol.ConsoleStreamEvent, error) {
	return protocol.StreamConsoleChat(ctx, token, payload, protocol.ConsoleStreamOptions{
		Poster:   consoleStreamPosterFactory(),
		TimeoutS: timeoutS,
	})
}

type consoleHTTPPoster struct{}

func (consoleHTTPPoster) PostConsoleStream(ctx context.Context, request protocol.ConsoleStreamRequest) (protocol.ConsoleStreamResponse, error) {
	payload, err := json.Marshal(request.Payload)
	if err != nil {
		return protocol.ConsoleStreamResponse{}, err
	}
	endpoint := consoleHTTPEndpoint()
	stream, err := transport.PostStream(ctx, endpoint, request.Token, payload, consoleHTTPOptions(request))
	if err != nil {
		logConsoleTransportError(endpoint, err)
		return protocol.ConsoleStreamResponse{}, err
	}
	defer stream.Close()

	lines := []string{}
	for {
		line, ok, err := stream.Next()
		if err != nil {
			return protocol.ConsoleStreamResponse{}, err
		}
		if !ok {
			return protocol.ConsoleStreamResponse{StatusCode: 200, Lines: lines}, nil
		}
		lines = append(lines, line)
	}
}

func consoleHTTPOptions(request protocol.ConsoleStreamRequest) transport.HTTPOptions {
	timeout := time.Duration(request.TimeoutS * float64(time.Second))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	endpoints := reverseruntime.GlobalEndpointTable()
	return transport.HTTPOptions{
		Lease:          proxyLeasePtr(request.Lease),
		Timeout:        timeout,
		ContentType:    "application/json",
		ConsoleHeaders: true,
		ExtraHeaders: map[string]string{
			"x-cluster": endpoints.Resolve("console_cluster"),
		},
	}
}

func consoleHTTPEndpoint() string {
	return reverseruntime.GlobalEndpointTable().Resolve("console_responses")
}

func proxyLeasePtr(lease controlproxy.ProxyLease) *controlproxy.ProxyLease {
	return &lease
}

func logConsoleTransportError(endpoint string, err error) {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		logging.Logger.Warn(
			"console upstream request failed",
			"endpoint", endpoint,
			"status", upstream.Status,
			"body_len", len(upstream.Body),
			"body_sha256", redact.SHA256Hex(upstream.Body),
			"body_excerpt", redact.Excerpt(upstream.Body, 400),
		)
		return
	}
	logging.Logger.Warn(
		"console transport request failed",
		"endpoint", endpoint,
		"error", redact.Excerpt(err.Error(), 400),
	)
}
