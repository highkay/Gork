package openai

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
)

const (
	maxLiteDiagnosticText     = 160
	maxLiteDiagnosticSnippets = 3
)

type liteStreamDiagnostic struct {
	dataFrames   int
	responseKeys map[string]int
	eventKinds   map[string]int
	textSnippets []string
}

func newLiteStreamDiagnostic() *liteStreamDiagnostic {
	return &liteStreamDiagnostic{
		responseKeys: map[string]int{},
		eventKinds:   map[string]int{},
	}
}

func (d *liteStreamDiagnostic) ObserveData(data string) {
	d.dataFrames++
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return
	}
	resp := liteDiagnosticNestedMap(obj, "result", "response")
	for key := range resp {
		d.responseKeys[key]++
	}
	if token := stringValue(resp["token"], ""); token != "" {
		d.addText(token)
	}
	if card, ok := resp["cardAttachment"].(map[string]any); ok {
		d.observeCard(card)
	}
}

func (d *liteStreamDiagnostic) ObserveEvents(events []protocol.FrameEvent) {
	for _, event := range events {
		d.eventKinds[event.Kind]++
		if event.Kind == "text" {
			d.addText(event.Content)
		}
	}
}

func (d *liteStreamDiagnostic) String() string {
	parts := []string{fmt.Sprintf("data_frames=%d", d.dataFrames)}
	if keys := liteDiagnosticCounts(d.responseKeys); keys != "" {
		parts = append(parts, "response_keys="+keys)
	}
	if kinds := liteDiagnosticCounts(d.eventKinds); kinds != "" {
		parts = append(parts, "event_kinds="+kinds)
	}
	if len(d.textSnippets) > 0 {
		parts = append(parts, fmt.Sprintf("text=%q", strings.Join(d.textSnippets, " | ")))
	}
	return strings.Join(parts, " ")
}

func (d *liteStreamDiagnostic) observeCard(card map[string]any) {
	raw := stringValue(card["jsonData"], "")
	if raw == "" {
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return
	}
	for key := range parsed {
		d.responseKeys["card."+key]++
	}
}

func (d *liteStreamDiagnostic) addText(value string) {
	text := strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if text == "" {
		return
	}
	if len(text) > maxLiteDiagnosticText {
		text = text[:maxLiteDiagnosticText]
	}
	for _, existing := range d.textSnippets {
		if existing == text {
			return
		}
	}
	if len(d.textSnippets) < maxLiteDiagnosticSnippets {
		d.textSnippets = append(d.textSnippets, text)
	}
}

func liteDiagnosticCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if counts[key] > 1 {
			parts = append(parts, fmt.Sprintf("%s(%d)", key, counts[key]))
			continue
		}
		parts = append(parts, key)
	}
	return strings.Join(parts, ",")
}

func liteDiagnosticNestedMap(data map[string]any, keys ...string) map[string]any {
	current := data
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}
