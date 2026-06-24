package transport

import "github.com/dslzl/gork/app/platform/redact"

const defaultTransportErrorExcerptBytes = 300

func redactedTransportExcerpt(value string) string {
	return redactedTransportExcerptLimit(value, defaultTransportErrorExcerptBytes)
}

func redactedTransportExcerptLimit(value string, limit int) string {
	if limit <= 0 {
		limit = defaultTransportErrorExcerptBytes
	}
	return redact.Excerpt(value, limit)
}

func redactedTransportError(err error) string {
	if err == nil {
		return "-"
	}
	return redactedTransportExcerpt(err.Error())
}
