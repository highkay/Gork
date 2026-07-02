package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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
		if request.HandleLine != nil {
			if err := request.HandleLine(line); err != nil {
				return nil, err
			}
			continue
		}
		lines = append(lines, line)
	}
}
