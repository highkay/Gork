package adapters

import (
	"fmt"
	"regexp"
	"strings"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
)

type ProxyProfile struct {
	CFCookies   string
	UserAgent   string
	CFClearance string
	Browser     string
}

type BrowserProfileData struct {
	Browser  string
	Brand    string
	Version  string
	Platform string
	Arch     string
	Mobile   bool
	Chromium bool
}

const DefaultConsoleUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

func ExtractCookieValue(cookieHeader, name string) string {
	if cookieHeader == "" {
		return ""
	}
	pattern := regexp.MustCompile(`(?:^|;\s*)` + regexp.QuoteMeta(name) + `=([^;]*)`)
	match := pattern.FindStringSubmatch(cookieHeader)
	if len(match) == 0 {
		return ""
	}
	return match[1]
}

func supportedBrowser(candidate string) string {
	return strings.TrimSpace(candidate)
}

func BrowserFromUserAgent(userAgent string) string {
	lower := strings.ToLower(userAgent)

	if match := regexp.MustCompile(`firefox/(\d+)`).FindStringSubmatch(lower); len(match) > 0 {
		return firstNonEmpty(supportedBrowser("firefox"+match[1]), supportedBrowser("firefox"))
	}

	if match := regexp.MustCompile(`edg/(\d+)`).FindStringSubmatch(lower); len(match) > 0 {
		return firstNonEmpty(supportedBrowser("edge"+match[1]), supportedBrowser("edge"))
	}

	if match := regexp.MustCompile(`(?:chrome|chromium|crios)/(\d+)`).FindStringSubmatch(lower); len(match) > 0 {
		suffix := ""
		if strings.Contains(lower, "android") {
			suffix = "_android"
		}
		exact := supportedBrowser("chrome" + match[1] + suffix)
		fallback := "chrome"
		if suffix != "" {
			fallback = "chrome_android"
		}
		return firstNonEmpty(exact, supportedBrowser(fallback))
	}

	safari := strings.Contains(lower, "safari/") &&
		!strings.Contains(lower, "chrome/") &&
		!strings.Contains(lower, "chromium/")
	if safari {
		if strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") {
			return supportedBrowser("safari_ios")
		}
		return supportedBrowser("safari")
	}

	return ""
}

func ResolveProxyProfile(lease *controlproxy.ProxyLease, configs ...controlproxy.ClearanceConfig) ProxyProfile {
	cfg := controlproxy.ResolveClearanceConfig(nil)
	if len(configs) > 0 {
		cfg = configs[0]
	}

	cookies := cfg.CFCookies
	userAgent := cfg.UserAgent
	clearance := cfg.CFClearance
	if lease != nil {
		if lease.CFCookies != "" {
			cookies = lease.CFCookies
		}
		if lease.UserAgent != "" {
			userAgent = lease.UserAgent
		}
		if value := ExtractCookieValue(lease.CFCookies, "cf_clearance"); value != "" {
			clearance = value
		}
	}

	browser := firstNonEmpty(
		BrowserFromUserAgent(userAgent),
		supportedBrowser(cfg.Browser),
		"chrome120",
	)
	return ProxyProfile{
		CFCookies:   cookies,
		UserAgent:   userAgent,
		CFClearance: clearance,
		Browser:     browser,
	}
}

func ResolveBrowserProfileData(profile ProxyProfile) BrowserProfileData {
	lowerBrowser := strings.ToLower(profile.Browser)
	lowerUA := strings.ToLower(profile.UserAgent)
	isChromium := containsAny(lowerBrowser, "chrome", "chromium", "edge", "brave") ||
		containsAny(lowerUA, "chrome", "chromium", "edg")
	if !isChromium || strings.Contains(lowerUA, "firefox") ||
		(strings.Contains(lowerUA, "safari") && !strings.Contains(lowerUA, "chrome")) {
		return BrowserProfileData{Browser: profile.Browser}
	}
	version := majorVersion(profile.Browser, profile.UserAgent)
	if version == "" {
		return BrowserProfileData{Browser: profile.Browser}
	}
	brand := "Google Chrome"
	switch {
	case strings.Contains(lowerBrowser, "edge") || strings.Contains(lowerUA, "edg"):
		brand = "Microsoft Edge"
	case strings.Contains(lowerBrowser, "brave"):
		brand = "Brave"
	case strings.Contains(lowerBrowser, "chromium"):
		brand = "Chromium"
	}
	platform := platformFromUA(profile.UserAgent)
	return BrowserProfileData{
		Browser:  profile.Browser,
		Brand:    brand,
		Version:  version,
		Platform: platform,
		Arch:     archFromUA(profile.UserAgent),
		Mobile:   strings.Contains(lowerUA, "mobile") || platform == "Android" || platform == "iOS",
		Chromium: true,
	}
}

func ClientHintsForProfile(profile ProxyProfile) map[string]string {
	data := ResolveBrowserProfileData(profile)
	if !data.Chromium || data.Version == "" {
		return map[string]string{}
	}
	mobile := "?0"
	if data.Mobile {
		mobile = "?1"
	}
	hints := map[string]string{
		"Sec-Ch-Ua":        fmt.Sprintf("\"%s\";v=\"%s\", \"Chromium\";v=\"%s\", \"Not(A:Brand\";v=\"24\"", data.Brand, data.Version, data.Version),
		"Sec-Ch-Ua-Mobile": mobile,
		"Sec-Ch-Ua-Model":  "",
	}
	if data.Platform != "" {
		hints["Sec-Ch-Ua-Platform"] = fmt.Sprintf("\"%s\"", data.Platform)
	}
	if data.Arch != "" {
		hints["Sec-Ch-Ua-Arch"] = data.Arch
		hints["Sec-Ch-Ua-Bitness"] = "64"
	}
	return hints
}

func clientHints(browser, ua string) map[string]string {
	return ClientHintsForProfile(ProxyProfile{Browser: browser, UserAgent: ua})
}

func majorVersion(browser, ua string) string {
	for _, src := range []string{browser, ua} {
		if match := regexp.MustCompile(`(\d{2,3})`).FindStringSubmatch(src); len(match) > 0 {
			return match[1]
		}
	}
	return ""
}

func platformFromUA(ua string) string {
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "windows"):
		return "Windows"
	case strings.Contains(lower, "mac os x") || strings.Contains(lower, "macintosh"):
		return "macOS"
	case strings.Contains(lower, "android"):
		return "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad"):
		return "iOS"
	case strings.Contains(lower, "linux"):
		return "Linux"
	default:
		return ""
	}
}

func archFromUA(ua string) string {
	lower := strings.ToLower(ua)
	if strings.Contains(lower, "aarch64") || strings.Contains(lower, "arm") {
		return "arm"
	}
	if strings.Contains(lower, "x86_64") || strings.Contains(lower, "x64") ||
		strings.Contains(lower, "win64") || strings.Contains(lower, "intel") {
		return "x86"
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
