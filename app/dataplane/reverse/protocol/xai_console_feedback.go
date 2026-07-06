package protocol

import "strings"

import controlproxy "github.com/dslzl/gork/app/control/proxy"

var consoleStatusFeedbackKinds = map[int]controlproxy.ProxyFeedbackKind{
	403: controlproxy.ProxyFeedbackChallenge,
	429: controlproxy.ProxyFeedbackRateLimited,
}

func ConsoleSuccessFeedback() controlproxy.ProxyFeedback {
	feedback := controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackSuccess)
	status := 200
	feedback.StatusCode = &status
	return feedback
}

func ConsoleTransportErrorFeedback() controlproxy.ProxyFeedback {
	return controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackTransportError)
}

func ConsoleStatusFeedback(status int, body ...string) controlproxy.ProxyFeedback {
	kind := consoleFeedbackKindForStatus(status, firstConsoleFeedbackBody(body))
	feedback := controlproxy.NewProxyFeedback(kind)
	feedback.StatusCode = &status
	return feedback
}

func consoleFeedbackKindForStatus(status int, body string) controlproxy.ProxyFeedbackKind {
	if status == 403 && isConsoleAccountForbidden(body) {
		return controlproxy.ProxyFeedbackForbidden
	}
	if kind, ok := consoleStatusFeedbackKinds[status]; ok {
		return kind
	}
	if status >= 500 {
		return controlproxy.ProxyFeedbackUpstream5xx
	}
	return controlproxy.ProxyFeedbackForbidden
}

func firstConsoleFeedbackBody(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func isConsoleAccountForbidden(body string) bool {
	text := strings.ToLower(body)
	for _, marker := range []string{
		`"code":"unauthorized:blocked-user"`,
		`"code":"account:`,
		"unauthorized:blocked-user",
		"account:email-domain-rejected",
		"user is blocked",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
