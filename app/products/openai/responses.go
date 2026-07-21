package openai

import (
	"context"
	"fmt"
	"strings"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/products"
)

type responseOptions struct {
	Model            string
	Input            any
	Instructions     string
	Stream           bool
	EmitThink        bool
	Temperature      float64
	TopP             float64
	Tools            []map[string]any
	ToolChoice       any
	PromptCacheSeed  string
	GrokTurnIndex    string
	RequestOverrides map[string]any
}

func Responses(ctx context.Context, options responseOptions) (chatCompletionResult, error) {
	// Build 原生 Responses 路径（不经 web/console SSO 池）。
	if model.IsBuildModelName(options.Model) {
		if !buildFeatureEnabled() {
			return chatCompletionResult{}, fmt.Errorf("Unknown model: '%s'", options.Model)
		}
		return BuildResponses(ctx, options)
	}
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return chatCompletionResult{}, err
	}
	messages := responseMessages(options.Instructions, options.Input)
	if spec.IsConsoleChat() {
		stream := options.Stream
		emitThink := options.EmitThink
		return ConsoleResponses(ctx, consoleResponseOptions{
			Model:       options.Model,
			Messages:    messages,
			Stream:      &stream,
			EmitThink:   &emitThink,
			Temperature: options.Temperature,
			TopP:        options.TopP,
			Tools:       options.Tools,
			ToolChoice:  options.ToolChoice,
			ResponseID:  MakeRespID("resp"),
			ReasoningID: MakeRespID("rs"),
			MessageID:   MakeRespID("msg"),
		})
	}

	directory := chatDirectoryProvider()
	if directory == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Account directory not initialised")
	}
	message, files := extractMessage(messages)
	if strings.TrimSpace(message) == "" {
		return chatCompletionResult{}, platform.NewUpstreamError("Empty message after extraction", 400, "")
	}
	toolNames := []string{}
	toolsDisabled := protocol.ToolChoiceDisablesTools(options.ToolChoice)
	if len(options.Tools) > 0 {
		chatTools := toResponseChatTools(options.Tools)
		if !toolsDisabled {
			toolNames = protocol.ExtractToolNames(chatTools)
		}
		message = protocol.InjectIntoMessage(message, protocol.BuildToolSystemPrompt(chatTools, options.ToolChoice))
	}

	ids := responseIDs{
		ResponseID:  MakeRespID("resp"),
		ReasoningID: MakeRespID("rs"),
		MessageID:   MakeRespID("msg"),
	}
	retryCodes := configuredRetryCodes(chatRetryConfig())
	dispatchDirectory := newChatDispatchDirectory(directory)
	return products.RunAccountDispatch(ctx, products.AccountDispatchOptions[chatCompletionResult]{
		Directory:         dispatchDirectory,
		Spec:              spec,
		Retry:             products.RetryPolicy{MaxAttempts: chatSelectionMaxRetries() + 1},
		Retryable:         func(err error) bool { return shouldRetryUpstream(err, retryCodes) },
		Feedback:          chatDispatchFeedback,
		NoAccountsMessage: "No available accounts for this model tier",
	}, func(ctx context.Context, lease products.AccountDispatchLease) (chatCompletionResult, error) {
		account, ok := dispatchDirectory.account(lease)
		if !ok {
			return chatCompletionResult{}, fmt.Errorf("missing response dispatch account for %s", lease.Token)
		}
		result, err := runResponseAttempt(ctx, responseAttemptOptions{
			Request:       options,
			Account:       account,
			Message:       message,
			Files:         files,
			IDs:           ids,
			ToolNames:     toolNames,
			ToolsDisabled: toolsDisabled,
		})
		if err == nil {
			quotaSync(ctx, account.Token, int(account.ModeID))
		} else {
			failSync(ctx, account.Token, int(account.ModeID), err)
		}
		return result, err
	})
}

type responseIDs struct {
	ResponseID  string
	ReasoningID string
	MessageID   string
}

type responseAttemptOptions struct {
	Request       responseOptions
	Account       chatAccount
	Message       string
	Files         []string
	IDs           responseIDs
	ToolNames     []string
	ToolsDisabled bool
}

func runResponseAttempt(ctx context.Context, options responseAttemptOptions) (chatCompletionResult, error) {
	lines, err := streamChat(ctx, chatStreamOptions{
		Token:          options.Account.Token,
		ModeID:         options.Account.ModeID,
		Message:        options.Message,
		Files:          options.Files,
		TimeoutSeconds: chatTimeoutSeconds(),
	})
	if err != nil {
		return chatCompletionResult{}, err
	}

	collected, frames, err := collectResponseStream(ctx, lines, options)
	if err != nil {
		return chatCompletionResult{}, err
	}
	output := responseOutputItems(options.IDs, collected.State, options.ToolNames, collected.ToolItems)
	outputTokens := estimateResponseOutputTokens(output, collected.State)
	usage := BuildRespUsage(
		platform.EstimatePromptTokens(options.Message, platform.PromptOverhead),
		outputTokens+platform.EstimateTokens(collected.State.Thinking),
		platform.EstimateTokens(collected.State.Thinking),
	)
	response := MakeRespObject(RespObjectParams{
		ResponseID: options.IDs.ResponseID,
		Model:      options.Request.Model,
		Status:     "completed",
		Output:     output,
		Usage:      usage,
	})
	if !options.Request.Stream {
		return chatCompletionResult{Response: response}, nil
	}
	frames = append(frames, FormatSSE("response.completed", map[string]any{
		"type":     "response.completed",
		"response": response,
	}), "data: [DONE]\n\n")
	return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
}
