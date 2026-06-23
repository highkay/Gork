package protocol

import (
	"strings"
	"testing"
)

func TestParseToolCallsCanonicalXMLMatchesPython(t *testing.T) {
	text := `<tool_calls>
		<tool_call>
			<tool_name>search</tool_name>
			<parameters>{"query":"golang","limit":2}</parameters>
		</tool_call>
	</tool_calls>`

	result := ParseToolCalls(text, nil)
	if !result.SawToolSyntax {
		t.Fatalf("SawToolSyntax = false, want true")
	}
	if len(result.Calls) != 1 {
		t.Fatalf("calls = %#v, want one", result.Calls)
	}
	call := result.Calls[0]
	if call.CallID == "" || call.Name != "search" || call.Arguments != `{"limit":2,"query":"golang"}` {
		t.Fatalf("call = %#v", call)
	}
}

func TestParseToolCallsJSONEnvelopeAndAvailableToolFilter(t *testing.T) {
	text := `prefix {"tool_calls":[{"name":"search","input":{"query":"ok"}},{"name":"skip","arguments":{"x":1}}]} suffix`

	result := ParseToolCalls(text, []string{"search"})
	if !result.SawToolSyntax {
		t.Fatalf("SawToolSyntax = false, want true")
	}
	if len(result.Calls) != 1 || result.Calls[0].Name != "search" {
		t.Fatalf("filtered calls = %#v", result.Calls)
	}
	if result.Calls[0].Arguments != `{"query":"ok"}` {
		t.Fatalf("arguments = %q", result.Calls[0].Arguments)
	}
}

func TestParseToolCallsMatchesPythonFalsyArgumentFallback(t *testing.T) {
	text := `{"tool_calls":[{"name":"bool_case","input":false,"arguments":{"fallback":true}},{"name":"zero_case","input":0,"parameters":{"n":1}}]}`

	result := ParseToolCalls(text, nil)
	if len(result.Calls) != 2 {
		t.Fatalf("calls = %#v, want two", result.Calls)
	}
	if result.Calls[0].Name != "bool_case" || result.Calls[0].Arguments != `{"fallback":true}` {
		t.Fatalf("bool fallback call = %#v", result.Calls[0])
	}
	if result.Calls[1].Name != "zero_case" || result.Calls[1].Arguments != `{"n":1}` {
		t.Fatalf("zero fallback call = %#v", result.Calls[1])
	}
}

func TestParseToolCallsBareJSONArrayMatchesPython(t *testing.T) {
	text := `tool_calls [{"tool_name":"lookup","parameters":{"id":7}},{"name":"","input":{}}]`

	result := ParseToolCalls(text, nil)
	if len(result.Calls) != 1 {
		t.Fatalf("calls = %#v, want one valid call", result.Calls)
	}
	if result.Calls[0].Name != "lookup" || result.Calls[0].Arguments != `{"id":7}` {
		t.Fatalf("call = %#v", result.Calls[0])
	}
}

func TestParseToolCallsAlternativeXMLMatchesPython(t *testing.T) {
	text := `<function_call><name>alpha</name><arguments>{"a":1}</arguments></function_call>
	<invoke name="beta">{"b":2}</invoke>
	<invoke name=gamma>not-json</invoke>`

	result := ParseToolCalls(text, nil)
	if len(result.Calls) != 3 {
		t.Fatalf("calls = %#v, want three", result.Calls)
	}
	if result.Calls[0].Name != "alpha" || result.Calls[0].Arguments != `{"a":1}` {
		t.Fatalf("function_call = %#v", result.Calls[0])
	}
	if result.Calls[1].Name != "beta" || result.Calls[1].Arguments != `{"b":2}` {
		t.Fatalf("invoke beta = %#v", result.Calls[1])
	}
	if result.Calls[2].Name != "gamma" || result.Calls[2].Arguments != `{}` {
		t.Fatalf("invoke gamma = %#v", result.Calls[2])
	}
}

func TestParseToolCallsSyntaxDetectionAndFailedParse(t *testing.T) {
	plain := ParseToolCalls("ordinary text", nil)
	if plain.SawToolSyntax || len(plain.Calls) != 0 {
		t.Fatalf("plain result = %#v, want no syntax and no calls", plain)
	}

	broken := ParseToolCalls(`tool_calls: not actually parseable`, nil)
	if !broken.SawToolSyntax || len(broken.Calls) != 0 {
		t.Fatalf("broken result = %#v, want syntax seen and no calls", broken)
	}
}

func TestParseToolCallsRepairsUnescapedNewlineInJSONStrings(t *testing.T) {
	text := `<tool_calls><tool_call><tool_name>note</tool_name><parameters>{"text":"hello
world"}</parameters></tool_call></tool_calls>`

	result := ParseToolCalls(text, nil)
	if len(result.Calls) != 1 {
		t.Fatalf("calls = %#v, want repaired call", result.Calls)
	}
	if result.Calls[0].Arguments != `{"text":"hello\nworld"}` {
		t.Fatalf("arguments = %q", result.Calls[0].Arguments)
	}
}

func TestParseToolCallsInvalidArgumentModes(t *testing.T) {
	text := `<tool_calls><tool_call><tool_name>lookup</tool_name><parameters>{"q":</parameters></tool_call></tool_calls>`

	fixed, err := ParseToolCallsWithOptions(text, nil, ToolCallParseOptions{InvalidArguments: ToolArgumentsFix})
	if err != nil || !fixed.SawToolSyntax || len(fixed.Calls) != 0 {
		t.Fatalf("fix mode result=%#v err=%v", fixed, err)
	}

	strict, err := ParseToolCallsWithOptions(text, nil, ToolCallParseOptions{InvalidArguments: ToolArgumentsError})
	if err == nil || !strict.SawToolSyntax {
		t.Fatalf("error mode result=%#v err=%v", strict, err)
	}

	downgraded, err := ParseToolCallsWithOptions(text, nil, ToolCallParseOptions{InvalidArguments: ToolArgumentsText})
	if err != nil || downgraded.SawToolSyntax || len(downgraded.Calls) != 0 {
		t.Fatalf("text mode result=%#v err=%v", downgraded, err)
	}
}

func FuzzParseToolCallsRobustInputs(f *testing.F) {
	for _, seed := range []string{
		`<tool_calls><tool_call><tool_name>search</tool_name><parameters>{"q":"x"}</parameters></tool_call></tool_calls>`,
		`{"tool_calls":[{"name":"search","input":{"q":"x"}}]}`,
		`{"tool_calls":[{"name":"search","input":{"q":`,
		`tool_calls [{"tool_name":"lookup","parameters":{"nested":{"a":[1,2,{"b":3}]}}}]`,
		`<tool_calls>` + strings.Repeat(`{"x":`, 64),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		_ = ParseToolCalls(text, []string{"search", "lookup"})
		_, _ = ParseToolCallsWithOptions(text, []string{"search", "lookup"}, ToolCallParseOptions{InvalidArguments: ToolArgumentsText})
	})
}
