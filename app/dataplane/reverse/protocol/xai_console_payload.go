package protocol

type ConsolePayloadOptions struct {
	Messages        []map[string]any
	Model           string
	Temperature     float64
	TopP            float64
	ReasoningEffort string
	Stream          *bool
	Tools           []map[string]any
	ToolChoice      any
}

type ConsoleRequestPayload struct {
	Model           string
	Input           []map[string]any
	MaxOutputTokens int
	Temperature     float64
	TopP            float64
	Store           bool
	Include         []any
	Stream          bool
	Reasoning       *ConsoleReasoningPayload
	Tools           []map[string]any
	ToolChoice      any
}

type ConsoleReasoningPayload struct {
	Effort string
}

func BuildConsolePayload(options ConsolePayloadOptions) map[string]any {
	return BuildConsoleRequestPayload(options).Map()
}

func BuildConsoleRequestPayload(options ConsolePayloadOptions) ConsoleRequestPayload {
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.7
	}
	topP := options.TopP
	if topP == 0 {
		topP = 0.95
	}
	stream := true
	if options.Stream != nil {
		stream = *options.Stream
	}
	inputItems := make([]map[string]any, 0, len(options.Messages))
	for _, message := range options.Messages {
		inputItems = append(inputItems, consoleInputItems(message)...)
	}
	effort := consoleModelFixedEffort[options.Model]
	if effort == "" {
		effort = consoleEffortMap[options.ReasoningEffort]
		if effort == "" {
			effort = "medium"
		}
	}
	consoleModel := ResolveConsoleModel(options.Model)
	maxTokens := consoleModelMaxOutputTokens[consoleModel]
	if maxTokens == 0 {
		maxTokens = 1000000
	}
	payload := ConsoleRequestPayload{
		Model:           consoleModel,
		Input:           inputItems,
		MaxOutputTokens: maxTokens,
		Temperature:     temperature,
		TopP:            topP,
		Store:           false,
		Include:         []any{"reasoning.encrypted_content"},
		Stream:          stream,
	}
	if _, ok := consoleModelsWithReasoning[consoleModel]; ok {
		payload.Reasoning = &ConsoleReasoningPayload{Effort: effort}
	}
	if _, ok := consoleModelsWithSearchTools[consoleModel]; ok {
		payload.Tools = []map[string]any{
			{"type": "web_search", "enable_image_understanding": true},
			{"type": "x_search", "enable_video_understanding": true},
		}
		payload.ToolChoice = "auto"
	}
	if len(options.Tools) > 0 {
		allowFunctionTools := ConsoleClientFunctionToolsEnabled(options.Model)
		payload.Tools = mergeConsoleTools(payload.Tools, consoleToolPayloads(options.Tools, allowFunctionTools))
		if allowFunctionTools {
			payload.ToolChoice = consoleToolChoice(options.ToolChoice)
		}
	}
	return payload
}

func (p ConsoleRequestPayload) Map() map[string]any {
	payload := map[string]any{
		"model":             p.Model,
		"input":             p.Input,
		"max_output_tokens": p.MaxOutputTokens,
		"temperature":       p.Temperature,
		"top_p":             p.TopP,
		"store":             p.Store,
		"include":           p.Include,
		"stream":            p.Stream,
	}
	if p.Reasoning != nil {
		payload["reasoning"] = map[string]any{"effort": p.Reasoning.Effort}
	}
	if len(p.Tools) > 0 {
		payload["tools"] = p.Tools
	}
	if p.ToolChoice != nil {
		payload["tool_choice"] = p.ToolChoice
	}
	return payload
}
