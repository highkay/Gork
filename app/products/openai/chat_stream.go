package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/logging"
)

func streamChat(ctx context.Context, options chatStreamOptions) ([]string, error) {
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

	response, err := streamPost(ctx, chatStreamRequest{
		Token: options.Token,
		Headers: map[string]string{
			"authorization": "Bearer " + options.Token,
			"content-type":  "application/json",
			"origin":        "https://grok.com",
			"referer":       "https://grok.com/",
		},
		PayloadBytes:   payloadBytes,
		TimeoutSeconds: options.TimeoutSeconds,
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
	logging.Logger.Info("streamChat upstream response",
		"line_count", len(response.Lines),
		"body_len", len(response.Body),
		"first_line", truncateFirstLine(response.Lines),
	)
	return append([]string{}, response.Lines...), nil
}

func defaultStreamPost(ctx context.Context, request chatStreamRequest) (*chatStreamResponse, error) {
	timeout := time.Duration(request.TimeoutSeconds * float64(time.Second))
	stream, err := transport.PostStream(ctx, chatStreamEndpoint(), request.Token, request.PayloadBytes, transport.HTTPOptions{
		Timeout:      timeout,
		ContentType:  "application/json",
		ExtraHeaders: request.Headers,
	})
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	lines := []string{}
	for {
		line, ok, err := stream.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return &chatStreamResponse{StatusCode: http.StatusOK, Lines: lines}, nil
		}
		lines = append(lines, line)
	}
}

func truncateFirstLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	first := lines[0]
	if len(first) > 200 {
		return first[:200]
	}
	return first
}
