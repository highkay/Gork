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
	consumer := newChatLineConsumer(options)
	frames := []string{}
	for _, line := range lines {
		lineFrames, err := consumer.Consume(line)
		if err != nil {
			return chatCompletionState{}, nil, err
		}
		frames = append(frames, lineFrames...)
		if consumer.done {
			break
		}
	}
	state, finalFrames, err := consumer.Finish()
	if err != nil {
		return chatCompletionState{}, nil, err
	}
	frames = append(frames, finalFrames...)
	return state, frames, nil
}

type chatLineConsumer struct {
	options          consumeChatLinesOptions
	text             *textRunLineConsumer
	toolCallsEmitted bool
	eventCursor      int
	done             bool
}

func newChatLineConsumer(options consumeChatLinesOptions) *chatLineConsumer {
	return &chatLineConsumer{
		options: options,
		text: newTextRunLineConsumer(textRunOptions{
			Context:           options.Context,
			Token:             options.Token,
			EmitThinking:      options.EmitThink,
			ThinkingSummary:   options.EmitThink,
			ShowSearchSources: true,
			ToolNames:         options.ToolNames,
			DisableTools:      options.DisableTools,
			EnableToolSieve:   options.IsStream,
		}),
	}
}

func (c *chatLineConsumer) Consume(line string) ([]string, error) {
	events, err := c.text.Consume(line)
	if err != nil {
		return nil, err
	}
	c.done = c.text.Done()
	frames := c.framesForEvents(events)
	c.eventCursor += len(events)
	return frames, nil
}

func (c *chatLineConsumer) Finish() (chatCompletionState, []string, error) {
	runState, events, err := c.text.Finish()
	if err != nil {
		return chatCompletionState{}, nil, err
	}

	frames := []string{}
	if c.eventCursor < len(events) {
		frames = c.framesForEvents(events[c.eventCursor:])
	}
	state := chatStateFromTextRun(runState)

	if c.options.IsStream && !c.toolCallsEmitted {
		final := MakeStreamChunk(StreamChunkParams{
			ResponseID:  c.options.ResponseID,
			Model:       c.options.Model,
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

func (c *chatLineConsumer) framesForEvents(events []textRunEvent) []string {
	if !c.options.IsStream {
		return nil
	}
	frames := []string{}
	for _, event := range events {
		if c.toolCallsEmitted {
			continue
		}
		switch event.Kind {
		case "text":
			if event.Content == "" {
				continue
			}
			frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
				ResponseID: c.options.ResponseID,
				Model:      c.options.Model,
				Content:    event.Content,
			})))
		case "thinking":
			if c.options.EmitThink && event.Content != "" {
				frames = append(frames, formatChatDataFrame(MakeThinkingChunk(ThinkingChunkParams{
					ResponseID: c.options.ResponseID,
					Model:      c.options.Model,
					Content:    event.Content,
				})))
			}
		case "tool_calls":
			frames = appendToolCallFrames(frames, c.options.ResponseID, c.options.Model, event.ToolCalls)
			c.toolCallsEmitted = true
		}
	}
	return frames
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
