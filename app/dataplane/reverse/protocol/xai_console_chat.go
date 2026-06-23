package protocol

var ConsoleModels = map[string]string{
	"grok-4.3-console":                     "grok-4.3",
	"grok-4.3-low":                         "grok-4.3",
	"grok-4.3-medium":                      "grok-4.3",
	"grok-4.3-high":                        "grok-4.3",
	"grok-4.20-0309-reasoning-console":     "grok-4.20-0309-reasoning",
	"grok-4.20-0309-console":               "grok-4.20-0309",
	"grok-4.20-0309-non-reasoning-console": "grok-4.20-0309-non-reasoning",
	"grok-4.20-multi-agent-console":        "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-low":            "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-medium":         "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-high":           "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-xhigh":          "grok-4.20-multi-agent-0309",
	"grok-build-console":                   "grok-build-0.1",
}

var consoleModelsWithReasoning = map[string]struct{}{
	"grok-4.3":                   {},
	"grok-4.20-multi-agent-0309": {},
}

var consoleModelFixedEffort = map[string]string{
	"grok-4.3-low":                 "low",
	"grok-4.3-medium":              "medium",
	"grok-4.3-high":                "high",
	"grok-4.20-multi-agent-low":    "low",
	"grok-4.20-multi-agent-medium": "medium",
	"grok-4.20-multi-agent-high":   "high",
	"grok-4.20-multi-agent-xhigh":  "xhigh",
}

var consoleModelMaxOutputTokens = map[string]int{
	"grok-4.20-multi-agent-0309": 2000000,
	"grok-build-0.1":             256000,
}

var consoleModelsWithSearchTools = map[string]struct{}{
	"grok-4.20-multi-agent-0309":   {},
	"grok-4.20-0309":               {},
	"grok-4.20-0309-reasoning":     {},
	"grok-4.20-0309-non-reasoning": {},
	"grok-4.3":                     {},
	"grok-build-0.1":               {},
}

var consoleEffortMap = map[string]string{
	"none":    "none",
	"minimal": "low",
	"low":     "low",
	"medium":  "medium",
	"high":    "high",
	"xhigh":   "xhigh",
}

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

type ConsoleStreamAdapter struct {
	TextBuf           []string
	Usage             map[string]any
	FunctionToolNames map[string]struct{}
	FunctionCalls     []ParsedToolCall
	functionByKey     map[string]*ParsedToolCall
	functionOrder     []string
	done              bool
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
	consoleModel := options.Model
	if mapped := ConsoleModels[options.Model]; mapped != "" {
		consoleModel = mapped
	}
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
		payload.Tools = mergeConsoleTools(payload.Tools, consoleToolPayloads(options.Tools))
		payload.ToolChoice = consoleToolChoice(options.ToolChoice)
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
