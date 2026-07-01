package openai

import "context"

type responseStreamResult struct {
	State     chatCompletionState
	ToolItems []map[string]any
}

func collectResponseStream(ctx context.Context, lines []string, options responseAttemptOptions) (responseStreamResult, []string, error) {
	runState, events, err := consumeTextRunLines(lines, textRunOptions{
		Context:           ctx,
		Token:             options.Account.Token,
		EmitThinking:      options.Request.EmitThink,
		ToolNames:         options.ToolNames,
		DisableTools:      options.ToolsDisabled,
		EnableToolSieve:   len(options.ToolNames) > 0 || options.ToolsDisabled,
		StopAfterToolCall: true,
	})
	if err != nil {
		return responseStreamResult{}, nil, err
	}

	frames := responseInitialFrames(options)
	toolItems := []map[string]any{}
	messageStarted := false
	reasoningStarted := false
	reasoningClosed := false
	toolCallsEmitted := false
	reasoningText := ""
	annotationIndex := 0

	for _, event := range events {
		switch event.Kind {
		case "thinking":
			if options.Request.EmitThink && event.Content != "" {
				if !reasoningStarted && options.Request.Stream {
					reasoningStarted = true
					frames = append(frames, responseReasoningStartFrames(options.IDs)...)
				}
				reasoningText += event.Content
				if options.Request.Stream {
					frames = append(frames, FormatSSE("response.reasoning_summary_text.delta", map[string]any{
						"type":          "response.reasoning_summary_text.delta",
						"item_id":       options.IDs.ReasoningID,
						"output_index":  0,
						"summary_index": 0,
						"delta":         event.Content,
					}))
				}
			}
		case "text":
			if reasoningStarted && !reasoningClosed && options.Request.Stream {
				reasoningClosed = true
				frames = append(frames, responseReasoningDoneFrames(options.IDs, reasoningText)...)
			}
			if event.Content == "" {
				continue
			}
			if !messageStarted && options.Request.Stream {
				messageStarted = true
				frames = append(frames, responseMessageStartFrames(options.IDs, responseMessageIndex(reasoningStarted))...)
			}
			if options.Request.Stream {
				frames = append(frames, responseTextDeltaFrame(options.IDs.MessageID, responseMessageIndex(reasoningStarted), event.Content))
			}
		case "annotation":
			if event.Annotation != nil {
				if options.Request.Stream && messageStarted {
					frames = append(frames, FormatSSE("response.output_text.annotation.added", map[string]any{
						"type":             "response.output_text.annotation.added",
						"item_id":          options.IDs.MessageID,
						"output_index":     responseMessageIndex(reasoningStarted),
						"content_index":    0,
						"annotation_index": annotationIndex,
						"annotation":       event.Annotation,
					}))
				}
				annotationIndex++
			}
		case "tool_calls":
			if !options.ToolsDisabled {
				toolItems = buildResponseFunctionCallItems(event.ToolCalls)
				frames = append(frames, emitResponseFunctionCallEvents(toolItems, responseMessageIndex(reasoningStarted))...)
				toolCallsEmitted = true
			}
		}
		if toolCallsEmitted {
			break
		}
	}

	state := chatStateFromTextRun(runState)
	for _, text := range state.ImageTexts {
		if options.Request.Stream && messageStarted && !toolCallsEmitted {
			frames = append(frames, responseTextDeltaFrame(options.IDs.MessageID, responseMessageIndex(reasoningStarted), text+"\n"))
		}
		if state.Text != "" {
			state.Text += "\n\n"
		}
		state.Text += text
	}
	if state.References != "" {
		state.Text += state.References
	}
	if options.Request.Stream && !toolCallsEmitted {
		msgIndex := responseMessageIndex(reasoningStarted)
		if !messageStarted {
			frames = append(frames, responseMessageStartFrames(options.IDs, msgIndex)...)
		}
		frames = append(frames, responseMessageDoneFrames(options.IDs, msgIndex, state)...)
	}
	return responseStreamResult{State: state, ToolItems: toolItems}, frames, nil
}
