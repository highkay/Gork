package platform

import "testing"

func TestMapHelpersReadTypedValues(t *testing.T) {
	payload := map[string]any{
		"name":    "grok",
		"enabled": true,
		"limit":   float64(12),
		"object":  map[string]any{"id": "nested"},
		"array":   []any{"a", "b"},
	}

	if got := String(payload, "name"); got != "grok" {
		t.Fatalf("String() = %q", got)
	}
	if got := Bool(payload, "enabled"); !got {
		t.Fatalf("Bool() = %v", got)
	}
	if got := Number(payload, "limit"); got != 12 {
		t.Fatalf("Number() = %v", got)
	}
	if got := Object(payload, "object"); got["id"] != "nested" {
		t.Fatalf("Object() = %#v", got)
	}
	if got := Array(payload, "array"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("Array() = %#v", got)
	}
}

func TestMapHelpersReturnZeroValuesForWrongTypes(t *testing.T) {
	payload := map[string]any{
		"name":    12,
		"enabled": "yes",
		"limit":   "12",
		"object":  []any{},
		"array":   map[string]any{},
	}

	if String(payload, "name") != "" || Bool(payload, "enabled") || Number(payload, "limit") != 0 {
		t.Fatalf("scalar helpers should return zero values for wrong types")
	}
	if Object(payload, "object") != nil {
		t.Fatalf("Object() should return nil for wrong type")
	}
	if Array(payload, "array") != nil {
		t.Fatalf("Array() should return nil for wrong type")
	}
}
