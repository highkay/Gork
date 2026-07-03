package protocol

import (
	"strings"
	"testing"
)

func BenchmarkParseToolCalls(b *testing.B) {
	text := `prefix {"tool_calls":[{"name":"search","input":{"query":"golang"}},{"name":"lookup","arguments":{"id":"42"}}]} suffix`
	tools := []string{"search", "lookup"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := ParseToolCalls(text, tools)
		if len(result.Calls) != 2 {
			b.Fatalf("calls = %d", len(result.Calls))
		}
	}
}

func BenchmarkParseToolCallsLargeNestedJSON(b *testing.B) {
	text := strings.Repeat("context ", 512) + `{"tool_calls":[{"name":"search","input":{"query":"golang","filters":{"nested":[1,2,{"lang":"go","tags":["runtime","parser"]}]}}},{"name":"lookup","arguments":{"ids":[1,2,3],"meta":{"source":"bench"}}}]}`
	tools := []string{"search", "lookup"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := ParseToolCalls(text, tools)
		if len(result.Calls) != 2 {
			b.Fatalf("calls = %d", len(result.Calls))
		}
	}
}

func BenchmarkParseToolCallsInvalidArguments(b *testing.B) {
	text := `<tool_calls><tool_call><tool_name>lookup</tool_name><parameters>{"q":</parameters></tool_call></tool_calls>`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseToolCallsWithOptions(text, nil, ToolCallParseOptions{InvalidArguments: ToolArgumentsError}); err == nil {
			b.Fatal("expected invalid arguments error")
		}
	}
}

func BenchmarkConsoleStreamAdapter(b *testing.B) {
	events := []struct {
		event string
		data  string
	}{
		{event: "response.output_text.delta", data: `{"delta":"hello "}`},
		{event: "response.output_item.added", data: `{"item":{"id":"call-1","type":"function_call","name":"lookup","call_id":"call-1","arguments":"{}"}}`},
		{event: "response.function_call_arguments.delta", data: `{"call_id":"call-1","delta":"{\"q\":\"go\"}"}`},
		{event: "response.completed", data: `{"response":{"usage":{"input_tokens":1,"output_tokens":2}}}`},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		adapter := NewConsoleStreamAdapter([]string{"lookup"})
		for _, event := range events {
			if _, err := adapter.Feed(event.event, event.data); err != nil {
				b.Fatalf("Feed returned error: %v", err)
			}
		}
		if adapter.FullText() != "hello " || len(adapter.FunctionCalls) != 1 {
			b.Fatalf("adapter result text=%q calls=%d", adapter.FullText(), len(adapter.FunctionCalls))
		}
	}
}

func BenchmarkConsoleStreamAdapterManyEvents(b *testing.B) {
	events := make([]struct {
		event string
		data  string
	}, 0, 128)
	for i := 0; i < 64; i++ {
		events = append(events, struct {
			event string
			data  string
		}{event: "response.output_text.delta", data: `{"delta":"hello "}`})
		events = append(events, struct {
			event string
			data  string
		}{event: "response.function_call_arguments.delta", data: `{"call_id":"call-1","delta":"{\"q\":\"go\"}"}`})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		adapter := NewConsoleStreamAdapter([]string{"lookup"})
		for _, event := range events {
			if _, err := adapter.Feed(event.event, event.data); err != nil {
				b.Fatal(err)
			}
		}
	}
}
