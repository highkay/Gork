package protocol

import controlproxy "github.com/dslzl/gork/app/control/proxy"

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
	kind := controlproxy.ProxyFeedbackForbidden
	if status == 403 {
		kind = controlproxy.ProxyFeedbackChallenge
	} else if status == 429 {
		kind = controlproxy.ProxyFeedbackRateLimited
	} else if status >= 500 {
		kind = controlproxy.ProxyFeedbackUpstream5xx
	}
	feedback := controlproxy.NewProxyFeedback(kind)
	feedback.StatusCode = &status
	return feedback
}
