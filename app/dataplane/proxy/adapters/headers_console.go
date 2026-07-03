package adapters

import reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"

func BuildConsoleHeaders(ssoToken string, options ...ConsoleHeaderOptions) map[string]string {
	opts := ConsoleHeaderOptions{ContentType: "application/json"}
	if len(options) > 0 {
		opts = options[0]
		if opts.ContentType == "" {
			opts.ContentType = "application/json"
		}
	}
	profile := resolveProfile(opts.Lease)
	ua := sanitize(&profile.UserAgent, false)
	if ua == "" {
		ua = DefaultConsoleUserAgent
	}
	table := reverseruntime.GlobalEndpointTable()
	headers := map[string]string{
		"Accept":          "*/*",
		"Accept-Encoding": "gzip, deflate, br, zstd",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Authorization":   "Bearer anonymous",
		"Content-Type":    opts.ContentType,
		"Cookie":          BuildSSOCookie(ssoToken, CookieOptions{Lease: opts.Lease}),
		"Origin":          table.Resolve("console_base"),
		"Priority":        "u=1, i",
		"Referer":         table.Resolve("console_referer"),
		"Sec-Fetch-Dest":  "empty",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Site":  "same-origin",
		"User-Agent":      ua,
		"x-cluster":       table.Resolve("console_cluster"),
	}
	for key, value := range ClientHintsForProfile(profile) {
		headers[key] = value
	}
	return headers
}
