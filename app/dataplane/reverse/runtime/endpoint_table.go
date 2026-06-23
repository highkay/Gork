package runtime

import (
	"strings"

	platformconfig "github.com/dslzl/gork/app/platform/config"
)

const (
	Base         = "https://grok.com"
	AssetsCDN    = "https://assets.grok.com"
	ConsoleBase  = "https://console.x.ai"
	AccountsBase = "https://accounts.x.ai"
)

const (
	Chat = Base + "/rest/app-chat/conversations/new"
)

const (
	AssetsUpload   = Base + "/rest/app-chat/upload-file"
	AssetsList     = Base + "/rest/assets"
	AssetsDelete   = Base + "/rest/assets-metadata"
	AssetsDownload = AssetsCDN
)

const (
	RateLimits = Base + "/rest/rate-limits"
)

const (
	AcceptTOS = AccountsBase + "/auth_mgmt.AuthManagement/SetTosAcceptedVersion"
	NSFWMgmt  = Base + "/auth_mgmt.AuthManagement/UpdateUserFeatureControls"
	SetBirth  = Base + "/rest/auth/set-birth-date"
)

const (
	MediaPost     = Base + "/rest/media/post/create"
	MediaPostLink = Base + "/rest/media/post/create-link"
	VideoUpscale  = Base + "/rest/media/video/upscale"
)

const (
	WSImagine = "wss://grok.com/ws/imagine/listen"
	WSLiveKit = "wss://livekit.grok.com"
)

const (
	LiveKitTokens = Base + "/rest/livekit/tokens"
)

const (
	ConsoleResponses  = ConsoleBase + "/v1/responses"
	ConsoleChat       = ConsoleBase + "/v1/chat/completions"
	ConsoleListModels = ConsoleBase + "/auth_mgmt.AuthManagement/ListModels"
)

type EndpointTable struct {
	values map[string]string
}

type EndpointConfigSource interface {
	GetStr(key string, defaultValue string) string
}

func DefaultEndpointTable() EndpointTable {
	return endpointTableFromBases(Base, AssetsCDN, ConsoleBase, AccountsBase, WSLiveKit)
}

func GlobalEndpointTable() EndpointTable {
	return ConfiguredEndpointTable(platformconfig.GlobalConfig)
}

func ConfiguredEndpointTable(config EndpointConfigSource) EndpointTable {
	if config == nil {
		return DefaultEndpointTable()
	}
	defaults := DefaultEndpointTable()
	overrides := map[string]string{}
	for name := range defaults.values {
		overrides[name] = config.GetStr("reverse.endpoints."+name, "")
	}
	return defaults.WithOverrides(overrides)
}

func (t EndpointTable) WithOverrides(overrides map[string]string) EndpointTable {
	base := t.Resolve("base")
	assetsCDN := t.Resolve("assets_cdn")
	consoleBase := t.Resolve("console_base")
	accountsBase := t.Resolve("accounts_base")
	wsLiveKit := t.Resolve("ws_livekit")
	for key, value := range overrides {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch key {
		case "base":
			base = strings.TrimRight(value, "/")
		case "assets_cdn":
			assetsCDN = strings.TrimRight(value, "/")
		case "console_base":
			consoleBase = strings.TrimRight(value, "/")
		case "accounts_base":
			accountsBase = strings.TrimRight(value, "/")
		case "ws_livekit":
			wsLiveKit = strings.TrimRight(value, "/")
		}
	}
	next := endpointTableFromBases(base, assetsCDN, consoleBase, accountsBase, wsLiveKit)
	for key, value := range overrides {
		value = strings.TrimSpace(value)
		if value == "" || !next.Has(key) {
			continue
		}
		next.values[key] = strings.TrimRight(value, "/")
	}
	return next
}

func (t EndpointTable) Resolve(name string) string {
	if t.values == nil {
		return DefaultEndpointTable().Resolve(name)
	}
	return t.values[name]
}

func (t EndpointTable) Has(name string) bool {
	if t.values == nil {
		return DefaultEndpointTable().Has(name)
	}
	_, ok := t.values[name]
	return ok
}

func endpointTableFromBases(base string, assetsCDN string, consoleBase string, accountsBase string, wsLiveKit string) EndpointTable {
	base = strings.TrimRight(base, "/")
	assetsCDN = strings.TrimRight(assetsCDN, "/")
	consoleBase = strings.TrimRight(consoleBase, "/")
	accountsBase = strings.TrimRight(accountsBase, "/")
	wsLiveKit = strings.TrimRight(wsLiveKit, "/")
	return EndpointTable{values: map[string]string{
		"base":                base,
		"base_referer":        base + "/",
		"assets_cdn":          assetsCDN,
		"console_base":        consoleBase,
		"console_referer":     consoleBase + "/",
		"accounts_base":       accountsBase,
		"chat":                base + "/rest/app-chat/conversations/new",
		"assets_upload":       base + "/rest/app-chat/upload-file",
		"assets_list":         base + "/rest/assets",
		"assets_delete":       base + "/rest/assets-metadata",
		"assets_download":     assetsCDN,
		"files_referer":       base + "/files",
		"rate_limits":         base + "/rest/rate-limits",
		"accept_tos":          accountsBase + "/auth_mgmt.AuthManagement/SetTosAcceptedVersion",
		"nsfw_mgmt":           base + "/auth_mgmt.AuthManagement/UpdateUserFeatureControls",
		"set_birth":           base + "/rest/auth/set-birth-date",
		"media_post":          base + "/rest/media/post/create",
		"media_post_link":     base + "/rest/media/post/create-link",
		"video_upscale":       base + "/rest/media/video/upscale",
		"imagine_referer":     base + "/imagine",
		"ws_imagine":          strings.Replace(base, "https://", "wss://", 1) + "/ws/imagine/listen",
		"ws_livekit":          wsLiveKit,
		"livekit_tokens":      base + "/rest/livekit/tokens",
		"console_responses":   consoleBase + "/v1/responses",
		"console_chat":        consoleBase + "/v1/chat/completions",
		"console_list_models": consoleBase + "/auth_mgmt.AuthManagement/ListModels",
		"console_cluster":     "https://us-east-1.api.x.ai",
	}}
}
