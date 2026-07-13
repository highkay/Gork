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
	got := extractClearanceCookies(selectClearanceCookies(cookies, "grok.com"))
	if got != "match=1" {
		t.Fatalf("selected cookies = %q, want match=1", got)
	}
}

func TestSelectClearanceCookiesFallsBackWhenNoHostMatch(t *testing.T) {
	cookies := []clearanceCookie{
		{Name: "one", Value: "1", Domain: "example.com"},
		{Name: "two", Value: "2", Domain: "example.org"},
	}
	got := extractClearanceCookies(selectClearanceCookies(cookies, "grok.com"))
	if got != "one=1; two=2" {
		t.Fatalf("selected cookies = %q, want all cookies", got)
	}
}
