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
	consumer := newTextRunLineConsumer(options)
	for _, line := range lines {
		if _, err := consumer.Consume(line); err != nil {
			return textRunState{}, nil, err
		}
		if consumer.Done() {
			break
		}
	}
	return consumer.Finish()
}

type textRunLineConsumer struct {
	options          textRunOptions
	adapter          *protocol.StreamAdapter
	eventsOut        []textRunEvent
	sieve            *ToolSieve
	toolCallsEmitted bool
	done             bool
}

func newTextRunLineConsumer(options textRunOptions) *textRunLineConsumer {
	consumer := &textRunLineConsumer{
		options: options,
		adapter: protocol.NewStreamAdapter(protocol.StreamAdapterOptions{
			ThinkingSummary:   options.ThinkingSummary,
			ShowSearchSources: options.ShowSearchSources,
		}),
	}
	if options.EnableToolSieve && (len(options.ToolNames) > 0 || options.DisableTools) {
		consumer.sieve = NewToolSieve(options.ToolNames)
	}
	return consumer
}

func (c *textRunLineConsumer) Consume(line string) ([]textRunEvent, error) {
	if c.done {
		return nil, nil
	}
	eventType, data := protocol.ClassifyLine(line)
	if eventType == "done" {
		c.done = true
		return nil, nil
	}
	if eventType != "data" || data == "" {
		return nil, nil
	}
	events, err := c.adapter.Feed(data)
	if err != nil {
		return nil, err
	}
	start := len(c.eventsOut)
	for _, event := range events {
		if c.toolCallsEmitted && c.options.StopAfterToolCall {
			continue
		}
		switch event.Kind {
		case "thinking":
			if c.options.EmitThinking && event.Content != "" {
				c.eventsOut = append(c.eventsOut, textRunEvent{Kind: "thinking", Content: event.Content})
			}
		case "text":
			c.consumeTextEvent(event.Content)
		case "annotation":
			if event.AnnotationData != nil {
				c.eventsOut = append(c.eventsOut, textRunEvent{Kind: "annotation", Annotation: event.AnnotationData})
			}
		case "soft_stop":
		}
		if c.toolCallsEmitted && c.options.StopAfterToolCall {
			c.done = true
			break
		}
	}
	return c.eventsOut[start:], nil
}

func (c *textRunLineConsumer) Done() bool {
	return c.done
}

func (c *textRunLineConsumer) consumeTextEvent(text string) {
	if c.sieve != nil {
		safeText, calls := c.sieve.Feed(text)
		if calls != nil {
			if safeText != "" {
				c.eventsOut = append(c.eventsOut, textRunEvent{Kind: "text", Content: safeText})
			}
			if !c.options.DisableTools {
				c.eventsOut = append(c.eventsOut, textRunEvent{Kind: "tool_calls", ToolCalls: calls})
				c.toolCallsEmitted = true
			}
			return
		}
		text = safeText
	}
	if text != "" {
		c.eventsOut = append(c.eventsOut, textRunEvent{Kind: "text", Content: text})
	}
}

func (c *textRunLineConsumer) Finish() (textRunState, []textRunEvent, error) {
	if c.sieve != nil && !c.toolCallsEmitted && !c.options.DisableTools {
		if calls := c.sieve.Flush(); calls != nil {
			c.eventsOut = append(c.eventsOut, textRunEvent{Kind: "tool_calls", ToolCalls: calls})
			c.toolCallsEmitted = true
		}
	}

	state := textRunState{
		Text:          strings.Join(c.adapter.TextBuf, ""),
		References:    c.adapter.ReferencesSuffix(),
		Annotations:   c.adapter.AnnotationsList(),
		SearchSources: c.adapter.SearchSourcesList(),
	}
	if c.options.EmitThinking {
		state.Thinking = strings.Join(c.adapter.ThinkingBuf, "")
	}
	if c.options.DisableTools {
		state.Text = suppressToolSyntax(state.Text)
		state.ToolSyntaxSuppressed = true
	}
	if c.toolCallsEmitted {
		for _, event := range c.eventsOut {
			if event.Kind == "tool_calls" {
				state.ToolCalls = event.ToolCalls
				break
			}
		}
	}
	resolve := c.options.ResolveImage
	if resolve == nil {
		resolve = resolveImage
	}
	ctx := c.options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	for _, image := range c.adapter.ImageURLs {
		resolved, err := resolve(ctx, c.options.Token, image.URL, image.ImageID)
		if err == nil && resolved != "" {
			state.ImageTexts = append(state.ImageTexts, resolved)
		}
	}
	return state, c.eventsOut, nil
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
