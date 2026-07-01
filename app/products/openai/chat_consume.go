package openai

import (
	"context"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
)

type consumeChatLinesOptions struct {
	Context      context.Context
	Token        string
	Model        string
	ResponseID   string
	EmitThink    bool
	IsStream     bool
	ToolNames    []string
	DisableTools bool
}

func consumeChatLines(lines []string, options consumeChatLinesOptions) (chatCompletionState, []string, error) {
	runState, events, err := consumeTextRunLines(lines, textRunOptions{
		Context:           options.Context,
		Token:             options.Token,
		EmitThinking:      options.EmitThink,
		ThinkingSummary:   options.EmitThink,
		ShowSearchSources: true,
		ToolNames:         options.ToolNames,
		DisableTools:      options.DisableTools,
		EnableToolSieve:   options.IsStream,
	})
	if err != nil {
		return chatCompletionState{}, nil, err
	}

	frames := []string{}
	toolCallsEmitted := false
	if options.IsStream {
		for _, event := range events {
			if toolCallsEmitted {
				continue
			}
			switch event.Kind {
			case "text":
				if event.Content == "" {
					continue
				}
				frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
					ResponseID: options.ResponseID,
					Model:      options.Model,
					Content:    event.Content,
				})))
			case "thinking":
				if options.EmitThink && event.Content != "" {
					frames = append(frames, formatChatDataFrame(MakeThinkingChunk(ThinkingChunkParams{
						ResponseID: options.ResponseID,
						Model:      options.Model,
						Content:    event.Content,
					})))
				}
			case "tool_calls":
				frames = appendToolCallFrames(frames, options.ResponseID, options.Model, event.ToolCalls)
				toolCallsEmitted = true
			}
		}
	}

	state := chatStateFromTextRun(runState)

	if options.IsStream && !toolCallsEmitted {
		final := MakeStreamChunk(StreamChunkParams{
			ResponseID:  options.ResponseID,
			Model:       options.Model,
			Content:     "",
			IsFinal:     true,
			Annotations: toChatAnnotations(state.Annotations),
		})
		if len(state.SearchSources) > 0 {
			final["search_sources"] = state.SearchSources
		}
		frames = append(frames, formatChatDataFrame(final), "data: [DONE]\n\n")
	}
	return state, frames, nil
}

func appendToolCallFrames(frames []string, responseID string, modelName string, calls []protocol.ParsedToolCall) []string {
	for i, call := range calls {
		frames = append(frames, formatChatDataFrame(MakeToolCallChunk(ToolCallChunkParams{
			ResponseID: responseID,
			Model:      modelName,
			Index:      i,
			CallID:     call.CallID,
			Name:       call.Name,
			Arguments:  call.Arguments,
			IsFirst:    true,
		})))
	}
	frames = append(frames, formatChatDataFrame(MakeToolCallDoneChunk(ToolCallDoneChunkParams{
		ResponseID: responseID,
		Model:      modelName,
	})), "data: [DONE]\n\n")
	return frames
}

func formatChatDataFrame(payload any) string {
	data, err := marshalCompactJSON(payload)
	if err != nil {
		data = "null"
	}
	return "data: " + data + "\n\n"
}
