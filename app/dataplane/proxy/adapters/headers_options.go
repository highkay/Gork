package adapters

import (
	"regexp"
	"strings"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
)

type CookieOptions struct {
	Lease       *controlproxy.ProxyLease
	CFCookies   *string
	CFClearance *string
}

type HTTPHeaderOptions struct {
	ContentType *string
	Origin      string
	Referer     string
	Lease       *controlproxy.ProxyLease
}

type WSHeaderOptions struct {
	Origin string
	Extra  map[string]string
	Lease  *controlproxy.ProxyLease
}

type ConsoleHeaderOptions struct {
	Lease       *controlproxy.ProxyLease
	ContentType string
}

func sanitize(value *string, field string, stripSpaces bool) string {
	raw := ""
	if value != nil {
		raw = *value
	}
	_ = field

	translated := strings.Map(normalizeHeaderRune, raw)
	if stripSpaces {
		translated = regexp.MustCompile(`\s+`).ReplaceAllString(translated, "")
	} else {
		translated = strings.TrimSpace(translated)
	}
	return strings.Map(func(r rune) rune {
		if r <= 0xff {
			return r
		}
		return -1
	}, translated)
}

func normalizeHeaderRune(r rune) rune {
	switch r {
	case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2212':
		return '-'
	case '\u2018', '\u2019':
		return '\''
	case '\u201c', '\u201d':
		return '"'
	case '\u00a0', '\u2007', '\u202f':
		return ' '
	case '\u200b', '\u200c', '\u200d', '\ufeff':
		return -1
	default:
		return r
	}
}

func resolveProfile(lease *controlproxy.ProxyLease) ProxyProfile {
	return ResolveProxyProfile(lease)
}
