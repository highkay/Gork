package providers

import (
	"strings"
	"testing"
)

func TestReadClearanceSolverResponseBodyRejectsOversize(t *testing.T) {
	_, err := readClearanceSolverResponseBody(strings.NewReader(strings.Repeat("x", int(maxClearanceSolverResponseBytes)+1)), "test")
	if err == nil {
		t.Fatal("readClearanceSolverResponseBody returned nil error for oversized body")
	}
}

func TestSelectClearanceCookiesPrefersTargetHost(t *testing.T) {
	cookies := []clearanceCookie{
		{Name: "match", Value: "1", Domain: ".grok.com"},
		{Name: "skip", Value: "2", Domain: "example.com"},
	}
	got := extractAllCookies(selectClearanceCookies(cookies, "grok.com"))
	if got != "match=1" {
		t.Fatalf("selected cookies = %q, want match=1", got)
	}
}

func TestSelectClearanceCookiesFallsBackWhenNoHostMatch(t *testing.T) {
	cookies := []clearanceCookie{
		{Name: "one", Value: "1", Domain: "example.com"},
		{Name: "two", Value: "2", Domain: "example.org"},
	}
	got := extractAllCookies(selectClearanceCookies(cookies, "grok.com"))
	if got != "one=1; two=2" {
		t.Fatalf("selected cookies = %q, want all cookies", got)
	}
}

func TestDecodeFallbacksAndFailures(t *testing.T) {
	result, ok, err := decodeFlareSolverrResponse([]byte(`not-json`), "https://grok.com")
	if err != nil || ok || result.Cookies != "" {
		t.Fatalf("decode invalid json = %#v ok=%v err=%v, want empty false nil", result, ok, err)
	}

	result, ok, err = decodeFlareSolverrResponse([]byte(`{"status":"ok","solution":{"cookies":[{"name":"cf_clearance","value":"abc","domain":".grok.com"}]}}`), "")
	if err != nil || !ok {
		t.Fatalf("decode default target ok=%v err=%v, want true nil", ok, err)
	}
	if result.ClearanceHost != "grok.com" || result.Cookies != "cf_clearance=abc" {
		t.Fatalf("decode result = %#v", result)
	}
}
