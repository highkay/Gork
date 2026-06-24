package protocol

type ConsoleStreamAdapter struct {
	TextBuf           []string
	Usage             map[string]any
	FunctionToolNames map[string]struct{}
	FunctionCalls     []ParsedToolCall
	functionByKey     map[string]*ParsedToolCall
	functionOrder     []string
	done              bool
}
