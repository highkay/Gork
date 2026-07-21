package build

import (
	"strings"
	"testing"
)

func TestNormalizeResponsesJSON(t *testing.T) {
	raw := []byte(`{"id":"resp_1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}`)
	got, err := NormalizeResponsesJSON("public-model", "resp_fallback", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["model"] != "public-model" || got["object"] != "response" || got["id"] != "resp_1" {
		t.Fatalf("got=%#v", got)
	}
}

func TestResponsesStreamFramesFromSSEText(t *testing.T) {
	sse := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed"}`,
		``,
	}, "\n")
	frames, err := ResponsesStreamFramesFromSSE("m", "resp_x", strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(frames, "")
	if !strings.Contains(joined, "response.created") || !strings.Contains(joined, "hello") || !strings.Contains(joined, "response.completed") {
		t.Fatalf("frames=%s", joined)
	}
}
