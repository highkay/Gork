package protocol

import "strings"

func ClassifyConsoleLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "skip", ""
	}
	if strings.HasPrefix(line, "event:") {
		return "event", strings.TrimSpace(line[6:])
	}
	if strings.HasPrefix(line, "data:") {
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" {
			return "done", ""
		}
		return "data", data
	}
	return "skip", ""
}
