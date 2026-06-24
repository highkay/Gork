package adapters

import reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"

func BuildWSHeaders(token string, options ...WSHeaderOptions) map[string]string {
	opts := WSHeaderOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	profile := resolveProfile(opts.Lease)
	rawUA := profile.UserAgent
	ua := sanitize(&rawUA, "user_agent", false)
	browser := profile.Browser
	table := reverseruntime.GlobalEndpointTable()
	origin := opts.Origin
	if origin == "" {
		origin = table.Resolve("base")
	}
	origin = sanitize(&origin, "origin", false)

	headers := map[string]string{
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Cache-Control":   "no-cache",
		"Origin":          origin,
		"Pragma":          "no-cache",
		"User-Agent":      ua,
	}
	for key, value := range ClientHintsForProfile(ProxyProfile{Browser: browser, UserAgent: rawUA}) {
		headers[key] = value
	}
	if token != "" {
		headers["Cookie"] = BuildSSOCookie(token, CookieOptions{Lease: opts.Lease})
	}
	for key, value := range opts.Extra {
		headers[key] = value
	}
	return headers
}
