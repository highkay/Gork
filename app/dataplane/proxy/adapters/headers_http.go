package adapters

import reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"

func BuildHTTPHeaders(cookieToken string, options ...HTTPHeaderOptions) map[string]string {
	opts := HTTPHeaderOptions{}
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
	referer := opts.Referer
	if referer == "" {
		referer = table.Resolve("base_referer")
	}
	origin = sanitize(&origin, "origin", false)
	referer = sanitize(&referer, "referer", false)

	contentType := "application/json"
	if opts.ContentType != nil && *opts.ContentType != "" {
		contentType = *opts.ContentType
	}
	accept := "*/*"
	fetchDest := "empty"
	switch contentType {
	case "image/jpeg", "image/png", "video/mp4", "video/webm":
		accept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"
		fetchDest = "document"
	}

	site := "same-site"
	if originHost(origin) != "" && originHost(origin) == originHost(referer) {
		site = "same-origin"
	}
	headers := map[string]string{
		"Accept":           accept,
		"Accept-Encoding":  "gzip, deflate, br, zstd",
		"Accept-Language":  "zh-CN,zh;q=0.9,en;q=0.8",
		"Baggage":          "sentry-environment=production,sentry-release=d6add6fb0460641fd482d767a335ef72b9b6abb8,sentry-public_key=b311e0f2690c81f25e2c4cf6d4f7ce1c",
		"Content-Type":     contentType,
		"Origin":           origin,
		"Priority":         "u=1, i",
		"Referer":          referer,
		"Sec-Fetch-Dest":   fetchDest,
		"Sec-Fetch-Mode":   "cors",
		"Sec-Fetch-Site":   site,
		"User-Agent":       ua,
		"x-statsig-id":     statsigID(),
		"x-xai-request-id": uuidString(),
	}
	for key, value := range ClientHintsForProfile(ProxyProfile{Browser: browser, UserAgent: rawUA}) {
		headers[key] = value
	}
	headers["Cookie"] = BuildSSOCookie(cookieToken, CookieOptions{Lease: opts.Lease})
	return headers
}
