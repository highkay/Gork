package protocol

import "testing"

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
