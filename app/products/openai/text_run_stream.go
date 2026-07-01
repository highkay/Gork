package openai

import (
	"context"
	"strings"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
)

type textRunImageResolver func(context.Context, string, string, string) (string, error)

type textRunOptions struct {
	Context           context.Context
	Token             string
	EmitThinking      bool
	ThinkingSummary   bool
	ShowSearchSources bool
	ToolNames         []string
	DisableTools      bool
	EnableToolSieve   bool
	StopAfterToolCall bool
	ResolveImage      textRunImageResolver
}

type textRunState struct {
	Text                 string
	Thinking             string
	ImageTexts           []string
	References           string
	Annotations          []map[string]any
	SearchSources        []map[string]any
	ToolCalls            []protocol.ParsedToolCall
	ToolSyntaxSuppressed bool
}

type textRunEvent struct {
	Kind       string
	Content    string
	Annotation map[string]any
	ToolCalls  []protocol.ParsedToolCall
}

func consumeTextRunLines(lines []string, options textRunOptions) (textRunState, []textRunEvent, error) {
	adapter := protocol.NewStreamAdapter(protocol.StreamAdapterOptions{
		ThinkingSummary:   options.ThinkingSummary,
		ShowSearchSources: options.ShowSearchSources,
	})
	eventsOut := []textRunEvent{}
	var sieve *ToolSieve
	if options.EnableToolSieve && (len(options.ToolNames) > 0 || options.DisableTools) {
		sieve = NewToolSieve(options.ToolNames)
	}
	toolCallsEmitted := false

	for _, line := range lines {
		eventType, data := protocol.ClassifyLine(line)
		if eventType == "done" {
			break
		}
		if eventType != "data" || data == "" {
			continue
		}
		events, err := adapter.Feed(data)
		if err != nil {
			return textRunState{}, nil, err
		}
		for _, event := range events {
			if toolCallsEmitted && options.StopAfterToolCall {
				continue
			}
			switch event.Kind {
			case "thinking":
				if options.EmitThinking && event.Content != "" {
					eventsOut = append(eventsOut, textRunEvent{Kind: "thinking", Content: event.Content})
				}
			case "text":
				text := event.Content
				if sieve != nil {
					safeText, calls := sieve.Feed(text)
					if calls != nil {
						if safeText != "" {
							eventsOut = append(eventsOut, textRunEvent{Kind: "text", Content: safeText})
						}
						if !options.DisableTools {
							eventsOut = append(eventsOut, textRunEvent{Kind: "tool_calls", ToolCalls: calls})
							toolCallsEmitted = true
							if options.StopAfterToolCall {
								break
							}
						}
						text = ""
					} else {
						text = safeText
					}
				}
				if text != "" {
					eventsOut = append(eventsOut, textRunEvent{Kind: "text", Content: text})
				}
			case "annotation":
				if event.AnnotationData != nil {
					eventsOut = append(eventsOut, textRunEvent{Kind: "annotation", Annotation: event.AnnotationData})
				}
			case "soft_stop":
			}
		}
		if toolCallsEmitted && options.StopAfterToolCall {
			break
		}
	}

	if sieve != nil && !toolCallsEmitted && !options.DisableTools {
		if calls := sieve.Flush(); calls != nil {
			eventsOut = append(eventsOut, textRunEvent{Kind: "tool_calls", ToolCalls: calls})
			toolCallsEmitted = true
		}
	}

	state := textRunState{
		Text:          strings.Join(adapter.TextBuf, ""),
		References:    adapter.ReferencesSuffix(),
		Annotations:   adapter.AnnotationsList(),
		SearchSources: adapter.SearchSourcesList(),
	}
	if options.EmitThinking {
		state.Thinking = strings.Join(adapter.ThinkingBuf, "")
	}
	if options.DisableTools {
		state.Text = suppressToolSyntax(state.Text)
		state.ToolSyntaxSuppressed = true
	}
	if toolCallsEmitted {
		for _, event := range eventsOut {
			if event.Kind == "tool_calls" {
				state.ToolCalls = event.ToolCalls
				break
			}
		}
	}
	resolve := options.ResolveImage
	if resolve == nil {
		resolve = resolveImage
	}
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	for _, image := range adapter.ImageURLs {
		resolved, err := resolve(ctx, options.Token, image.URL, image.ImageID)
		if err == nil && resolved != "" {
			state.ImageTexts = append(state.ImageTexts, resolved)
		}
	}
	return state, eventsOut, nil
}

func chatStateFromTextRun(state textRunState) chatCompletionState {
	return chatCompletionState{
		Text:          state.Text,
		Thinking:      state.Thinking,
		ImageTexts:    state.ImageTexts,
		References:    state.References,
		Annotations:   state.Annotations,
		SearchSources: state.SearchSources,
	}
}
