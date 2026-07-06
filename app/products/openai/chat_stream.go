package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
)

func streamChat(ctx context.Context, options chatStreamOptions) ([]string, error) {
	response, err := postChatStream(ctx, options, nil)
	if err != nil {
		return nil, err
	}
	return append([]string{}, response.Lines...), nil
}

func streamChatIncremental(ctx context.Context, options chatStreamOptions, handleLine func(string) error) error {
	response, err := postChatStream(ctx, options, handleLine)
	if err != nil {
		return err
	}
	for _, line := range response.Lines {
		if err := handleLine(line); err != nil {
			return err
		}
	}
	return nil
}

func postChatStream(ctx context.Context, options chatStreamOptions, handleLine func(string) error) (*chatStreamResponse, error) {
	attachments, err := prepareFileAttachments(ctx, options.Token, options.Files)
	if err != nil {
		return nil, err
	}

	payload := protocol.BuildChatPayload(protocol.ChatPayloadOptions{
		Message:             options.Message,
		ModeID:              options.ModeID,
		FileAttachments:     attachments,
		ToolOverrides:       options.ToolOverrides,
		ModelConfigOverride: options.ModelConfigOverride,
		RequestOverrides:    options.RequestOverrides,
	})
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	table := reverseruntime.GlobalEndpointTable()
	response, err := streamPost(ctx, chatStreamRequest{
		Token: options.Token,
		Headers: map[string]string{
			"authorization": "Bearer " + options.Token,
			"content-type":  "application/json",
			"origin":        table.Resolve("base"),
			"referer":       table.Resolve("base_referer"),
		},
		PayloadBytes:   payloadBytes,
		TimeoutSeconds: options.TimeoutSeconds,
		HandleLine:     handleLine,
	})
	if err != nil {
		return nil, transportUpstreamError(err, "Chat transport failed")
	}
	if response == nil {
		return nil, platform.NewUpstreamError("Chat upstream returned 502", 502, "")
	}
	if response.StatusCode != 200 {
		body := response.Body
		if len(body) > 400 {
			body = body[:400]
		}
		return nil, platform.NewUpstreamError(fmt.Sprintf("Chat upstream returned %d", response.StatusCode), response.StatusCode, body)
	}
	return response, nil
}

func defaultStreamPost(ctx context.Context, request chatStreamRequest) (*chatStreamResponse, error) {
	proxyRuntime, proxyLease, err := acquireChatProxyLease(ctx)
	if err != nil {
		return nil, err
	}
	proxyFeedback := controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackSuccess)
	defer func() {
		if proxyRuntime != nil && proxyLease != nil {
			_ = proxyRuntime.Feedback(ctx, *proxyLease, proxyFeedback)
		}
	}()

	timeout := time.Duration(request.TimeoutSeconds * float64(time.Second))
	stream, err := transport.PostStream(ctx, chatStreamEndpoint(), request.Token, request.PayloadBytes, transport.HTTPOptions{
		Timeout:      timeout,
		ContentType:  "application/json",
		ExtraHeaders: request.Headers,
		Lease:        proxyLease,
	})
	if err != nil {
		proxyFeedback = chatProxyFeedbackForError(err)
		return nil, err
	}
	defer stream.Close()

	lines := []string{}
	for {
		line, ok, err := stream.Next()
		if err != nil {
			proxyFeedback = controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackTransportError)
			return nil, err
		}
		if !ok {
			return &chatStreamResponse{StatusCode: http.StatusOK, Lines: lines}, nil
		}
		if request.HandleLine != nil {
			if err := request.HandleLine(line); err != nil {
				return nil, err
			}
			continue
		}
		lines = append(lines, line)
	}
}

func acquireChatProxyLease(ctx context.Context) (*controlproxy.ProxyDirectory, *controlproxy.ProxyLease, error) {
	proxyRuntime, err := defaultProxyTransportRuntime(ctx)
	if err != nil || proxyRuntime == nil {
		return proxyRuntime, nil, err
	}
	lease, err := proxyRuntime.Acquire(ctx, controlproxy.AcquireOptions{
		Scope: controlproxy.ProxyScopeApp,
		Kind:  controlproxy.RequestKindHTTP,
	})
	if err != nil {
		return proxyRuntime, nil, err
	}
	return proxyRuntime, &lease, nil
}

func chatProxyFeedbackForError(err error) controlproxy.ProxyFeedback {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) && upstream != nil && upstream.Status > 0 {
		if upstream.Status == http.StatusForbidden && isXAIAccountForbidden(upstream.Body) {
			feedback := controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackForbidden)
			feedback.StatusCode = &upstream.Status
			return feedback
		}
		return controlproxy.BuildFeedback(upstream.Status)
	}
	return controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackTransportError)
}

func isXAIAccountForbidden(body string) bool {
	text := strings.ToLower(body)
	for _, marker := range []string{
		`"code":"unauthorized:blocked-user"`,
		`"code":"account:`,
		"unauthorized:blocked-user",
		"account:email-domain-rejected",
		"user is blocked",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
