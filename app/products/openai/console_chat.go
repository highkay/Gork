package openai

import (
	"context"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/products"
)

var consoleStreamChat = func(ctx context.Context, token string, payload map[string]any, timeoutS float64) ([]protocol.ConsoleStreamEvent, error) {
	return products.StreamConsoleChat(ctx, token, payload, timeoutS)
}

func init() {
	consoleCompletions = ConsoleCompletions
}

func consoleReasoningEffort(emitThink *bool) string {
	if emitThink != nil && !*emitThink {
		return "none"
	}
	return "low"
}

func ConsoleCompletions(ctx context.Context, options chatCompletionOptions) (chatCompletionResult, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return chatCompletionResult{}, err
	}
	directory := chatDirectoryProvider()
	if directory == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Account directory not initialised")
	}

	isStream := false
	if options.Stream != nil {
		isStream = *options.Stream
	}
	maxRetries := chatSelectionMaxRetries()
	retryCodes := configuredRetryCodes(chatRetryConfig())
	responseID := chatResponseID()
	timeoutS := chatTimeoutSeconds()
	excluded := []string{}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		account, ok, err := directory.ReserveChatAccount(ctx, spec, excluded)
		if err != nil {
			return chatCompletionResult{}, err
		}
		if !ok {
			return chatCompletionResult{}, platform.NewRateLimitError("No available accounts for this model tier")
		}

		result, err := runConsoleCompletionAttempt(ctx, options, account, responseID, isStream, timeoutS)
		finishChatAttempt(ctx, directory, account, err == nil, err)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if shouldRetryUpstream(err, retryCodes) && attempt < maxRetries {
			excluded = append(excluded, account.Token)
			continue
		}
		return chatCompletionResult{}, err
	}
	if lastErr != nil {
		return chatCompletionResult{}, lastErr
	}
	return chatCompletionResult{}, platform.NewRateLimitError("No available accounts after retries")
}

func runConsoleCompletionAttempt(ctx context.Context, options chatCompletionOptions, account chatAccount, responseID string, isStream bool, timeoutS float64) (chatCompletionResult, error) {
	upstreamStream := true
	functionToolNames := []string{}
	if !protocol.ToolChoiceDisablesTools(options.ToolChoice) {
		functionToolNames = protocol.ClientFunctionToolNames(options.Tools)
	}
	payload := protocol.BuildConsolePayload(protocol.ConsolePayloadOptions{
		Messages:        options.Messages,
		Model:           options.Model,
		Temperature:     options.Temperature,
		TopP:            options.TopP,
		ReasoningEffort: consoleReasoningEffort(options.EmitThink),
		Stream:          &upstreamStream,
		Tools:           options.Tools,
		ToolChoice:      options.ToolChoice,
	})

	events, err := consoleStreamChat(ctx, account.Token, payload, timeoutS)
	if err != nil {
		return chatCompletionResult{}, err
	}

	adapter := protocol.NewConsoleStreamAdapter(functionToolNames)
	frames := []string{}
	buffered := []string{}
	for _, event := range events {
		tokens, err := adapter.Feed(event.EventType, event.Data)
		if err != nil {
			return chatCompletionResult{}, err
		}
		if isStream {
			for _, token := range tokens {
				if adapter.HasFunctionTools() {
					buffered = append(buffered, token)
					continue
				}
				frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
					ResponseID: responseID,
					Model:      options.Model,
					Content:    token,
				})))
			}
		}
	}

	usage := consoleUsage(adapter, options.Messages)
	if len(adapter.FunctionCalls) > 0 {
		toolCalls := consoleToolCallsAny(adapter.FunctionCalls)
		usage = BuildUsage(
			consoleInputTokens(adapter, options.Messages),
			platform.EstimateToolCallTokens(toolCalls),
		)
		if isStream {
			for index, call := range adapter.FunctionCalls {
				frames = append(frames, formatChatDataFrame(MakeToolCallChunk(ToolCallChunkParams{
					ResponseID: responseID,
					Model:      options.Model,
					Index:      index,
					CallID:     call.CallID,
					Name:       call.Name,
					Arguments:  call.Arguments,
					IsFirst:    true,
				})))
			}
			frames = append(frames, formatChatDataFrame(MakeToolCallDoneChunk(ToolCallDoneChunkParams{
				ResponseID: responseID,
				Model:      options.Model,
				Usage:      usage,
			})), "data: [DONE]\n\n")
			return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
		}
		return chatCompletionResult{Response: MakeToolCallResponse(ToolCallResponseParams{
			Model:         options.Model,
			ToolCalls:     toolCalls,
			PromptContent: options.Messages,
			ResponseID:    responseID,
			Usage:         usage,
		})}, nil
	}
	if isStream {
		for _, token := range buffered {
			frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
				ResponseID: responseID,
				Model:      options.Model,
				Content:    token,
			})))
		}
		frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
			ResponseID:   responseID,
			Model:        options.Model,
			Content:      "",
			IsFinal:      true,
			Usage:        usage,
			FinishReason: "stop",
		})), "data: [DONE]\n\n")
		return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
	}

	return chatCompletionResult{Response: MakeChatResponse(ChatResponseParams{
		Model:         options.Model,
		Content:       adapter.FullText(),
		PromptContent: options.Messages,
		ResponseID:    responseID,
		Usage:         usage,
	})}, nil
}

func consoleToolCallsAny(calls []protocol.ParsedToolCall) []any {
	toolCalls := make([]any, 0, len(calls))
	for _, call := range calls {
		toolCalls = append(toolCalls, call)
	}
	return toolCalls
}

func consoleInputTokens(adapter *protocol.ConsoleStreamAdapter, messages []map[string]any) int {
	if adapter.Usage != nil {
		if tokens := intFromAny(adapter.Usage["input_tokens"]); tokens > 0 {
			return tokens
		}
	}
	return platform.EstimatePromptTokens(messages, platform.PromptOverhead)
}

func consoleUsage(adapter *protocol.ConsoleStreamAdapter, messages []map[string]any) map[string]any {
	if adapter.Usage != nil {
		inputTokens := intFromAny(adapter.Usage["input_tokens"])
		outputTokens := intFromAny(adapter.Usage["output_tokens"])
		return BuildUsage(inputTokens, outputTokens)
	}
	return BuildUsage(platform.EstimatePromptTokens(messages, platform.PromptOverhead), platform.EstimateTokens(adapter.FullText()))
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
