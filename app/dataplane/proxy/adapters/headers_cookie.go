package adapters

import (
	"fmt"
	"regexp"
	"strings"
)

func BuildSSOCookie(ssoToken string, options ...CookieOptions) string {
	opts := CookieOptions{}
	if len(options) > 0 {
		opts = options[0]
	}

	cookie := normalizeSSOCookieInput(ssoToken)

	profile := resolveProfile(opts.Lease)
	cfCookies := profile.CFCookies
	if opts.CFCookies != nil {
		cfCookies = *opts.CFCookies
	}
	cfClearance := profile.CFClearance
	if opts.CFClearance != nil {
		cfClearance = *opts.CFClearance
	}
	effectiveCookies := sanitize(&cfCookies, false)
	effectiveClearance := sanitize(&cfClearance, true)
	effectiveClearance = strings.ReplaceAll(effectiveClearance, ";", "")

	if effectiveClearance != "" && effectiveCookies != "" {
		if regexp.MustCompile(`(?:^|;\s*)cf_clearance=`).MatchString(effectiveCookies) {
			effectiveCookies = replaceFirstCFClearance(effectiveCookies, effectiveClearance)
		} else {
			effectiveCookies = strings.TrimRight(effectiveCookies, "; ") + "; cf_clearance=" + effectiveClearance
		}
	} else if effectiveClearance != "" {
		effectiveCookies = "cf_clearance=" + effectiveClearance
	}

	if effectiveCookies != "" {
		cookie = mergeCookieStrings(cookie, effectiveCookies)
	}
	return cookie
}

func normalizeSSOCookieInput(ssoToken string) string {
	raw := sanitize(&ssoToken, false)
	if hasCookiePair(raw, "sso") || hasCookiePair(raw, "sso-rw") {
		return normalizeStoredSSOCookie(raw)
	}
	token := sanitize(&ssoToken, true)
	return fmt.Sprintf("sso=%s; sso-rw=%s", token, token)
}

func normalizeStoredSSOCookie(raw string) string {
	pairs := splitCookiePairs(raw)
	normalized := make([]string, 0, len(pairs)+1)
	ssoValue := ""
	hasSSORW := false
	for _, pair := range pairs {
		name, value, ok := splitCookiePair(pair)
		if !ok {
			continue
		}
		switch strings.ToLower(name) {
		case "sso":
			value = sanitize(&value, true)
			ssoValue = value
		case "sso-rw":
			value = sanitize(&value, true)
			hasSSORW = true
		}
		normalized = append(normalized, name+"="+value)
	}
	if ssoValue != "" && !hasSSORW {
		normalized = append(normalized, "sso-rw="+ssoValue)
	}
	return strings.Join(normalized, "; ")
}

func hasCookiePair(cookies string, name string) bool {
	pattern := `(?:^|;\s*)` + regexp.QuoteMeta(name) + `=`
	return regexp.MustCompile(pattern).MatchString(cookies)
}

func splitCookiePairs(cookies string) []string {
	rawPairs := strings.Split(cookies, ";")
	pairs := make([]string, 0, len(rawPairs))
	for _, pair := range rawPairs {
		pair = strings.TrimSpace(pair)
		if pair != "" {
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

func splitCookiePair(pair string) (string, string, bool) {
	name, value, ok := strings.Cut(pair, "=")
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if !ok || name == "" {
		return "", "", false
	}
	return name, value, true
}

func mergeCookieStrings(base string, extra string) string {
	merged := base
	for _, pair := range splitCookiePairs(extra) {
		name, value, ok := splitCookiePair(pair)
		if !ok {
			continue
		}
		merged = upsertCookiePair(merged, name, value)
	}
	return merged
}

func upsertCookiePair(cookies string, name string, value string) string {
	if cookies == "" {
		return name + "=" + value
	}
	pattern := `(^|;\s*)` + regexp.QuoteMeta(name) + `=[^;]*`
	re := regexp.MustCompile(pattern)
	if re.MatchString(cookies) {
		return re.ReplaceAllString(cookies, "${1}"+name+"="+escapeReplacement(value))
	}
	return strings.TrimRight(cookies, "; ") + "; " + name + "=" + value
}

func replaceFirstCFClearance(cookies string, clearance string) string {
	pairs := splitCookiePairs(cookies)
	out := make([]string, 0, len(pairs))
	replaced := false
	for _, pair := range pairs {
		name, _, ok := splitCookiePair(pair)
		if ok && strings.EqualFold(name, "cf_clearance") {
			if !replaced {
				out = append(out, "cf_clearance="+clearance)
				replaced = true
			}
			continue
		}
		out = append(out, strings.TrimSpace(pair))
	}
	if !replaced {
		out = append(out, "cf_clearance="+clearance)
	}
	return strings.Join(out, "; ")
}

func escapeReplacement(value string) string {
	return strings.ReplaceAll(value, "$", "$$")
}
