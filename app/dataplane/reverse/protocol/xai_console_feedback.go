package protocol

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

func ConsoleStatusFeedback(status int) controlproxy.ProxyFeedback {
	kind := consoleFeedbackKindForStatus(status)
	feedback := controlproxy.NewProxyFeedback(kind)
	feedback.StatusCode = &status
	return feedback
}

func consoleFeedbackKindForStatus(status int) controlproxy.ProxyFeedbackKind {
	if kind, ok := consoleStatusFeedbackKinds[status]; ok {
		return kind
	}
	if status >= 500 {
		return controlproxy.ProxyFeedbackUpstream5xx
	}
	return controlproxy.ProxyFeedbackForbidden
}
