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

func ResolveConsoleModel(model string) string {
	if mapped := ConsoleModels[model]; mapped != "" {
		return mapped
	}
	return model
}

var consoleModelsWithReasoning = map[string]struct{}{
	"grok-4.3":                   {},
	"grok-4.20-multi-agent-0309": {},
}

var consoleModelsWithoutClientFunctionTools = map[string]struct{}{
	"grok-4.20-multi-agent-0309": {},
}

func ConsoleClientFunctionToolsEnabled(model string) bool {
	_, disabled := consoleModelsWithoutClientFunctionTools[ResolveConsoleModel(model)]
	return !disabled
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
