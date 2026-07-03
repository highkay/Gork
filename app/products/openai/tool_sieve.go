package openai

import (
	"regexp"
	"strings"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
)

var (
	toolOpenTagRE  = regexp.MustCompile(`(?i)<tool_calls[\s>]?`)
	toolCloseTagRE = regexp.MustCompile(`(?i)</tool_calls\s*>`)
)

const maxToolSieveBufferBytes = 64 << 10

type ToolSieve struct {
	toolNames []string
	buf       string
	capturing bool
	done      bool
}

func NewToolSieve(toolNames []string) *ToolSieve {
	return &ToolSieve{toolNames: toolNames}
}

func suppressToolSyntax(text string) string {
	sieve := NewToolSieve(nil)
	safe, _ := sieve.Feed(text)
	_ = sieve.Flush()
	return safe
}

func (s *ToolSieve) Feed(chunk string) (string, []protocol.ParsedToolCall) {
	if s.done || chunk == "" {
		if s.capturing {
			return "", nil
		}
		return chunk, nil
	}

	if s.capturing {
		return s.feedCapturing(chunk)
	}
	return s.feedScanning(chunk)
}

func (s *ToolSieve) Flush() []protocol.ParsedToolCall {
	if s.done || s.buf == "" {
		return nil
	}
	s.done = true
	result := protocol.ParseToolCalls(s.buf, s.toolNames)
	s.buf = ""
	if result.SawToolSyntax {
		return callsOrEmpty(result.Calls)
	}
	return nil
}

func (s *ToolSieve) feedScanning(chunk string) (string, []protocol.ParsedToolCall) {
	combined := s.buf + chunk
	s.buf = ""

	match := toolOpenTagRE.FindStringIndex(combined)
	if match == nil {
		safe, leftover := splitAtBoundary(combined, "<tool_calls")
		s.buf = leftover
		return safe, nil
	}

	safePart := combined[:match[0]]
	s.buf = combined[match[0]:]
	s.capturing = true
	capSafe, calls := s.feedCapturing("")
	return safePart + capSafe, calls
}

func (s *ToolSieve) feedCapturing(chunk string) (string, []protocol.ParsedToolCall) {
	s.buf += chunk
	if len(s.buf) > maxToolSieveBufferBytes {
		s.buf = ""
		s.capturing = false
		s.done = true
		return "", nil
	}
	match := toolCloseTagRE.FindStringIndex(s.buf)
	if match == nil {
		return "", nil
	}

	xmlBlock := s.buf[:match[1]]
	s.buf = ""
	s.capturing = false
	s.done = true

	result := protocol.ParseToolCalls(xmlBlock, s.toolNames)
	if result.SawToolSyntax {
		return "", callsOrEmpty(result.Calls)
	}
	return "", nil
}

func splitAtBoundary(text string, prefix string) (string, string) {
	maxLength := len(prefix) - 1
	if len(text) < maxLength {
		maxLength = len(text)
	}
	for i := maxLength; i > 0; i-- {
		if strings.HasSuffix(text, prefix[:i]) {
			return text[:len(text)-i], text[len(text)-i:]
		}
	}
	return text, ""
}

func callsOrEmpty(calls []protocol.ParsedToolCall) []protocol.ParsedToolCall {
	if calls == nil {
		return []protocol.ParsedToolCall{}
	}
	return calls
}
