package runtime

import "testing"

func TestEndpointTableMatchesPythonConstants(t *testing.T) {
	tests := map[string]string{
		"Base":             Base,
		"AssetsCDN":        AssetsCDN,
		"ConsoleBase":      ConsoleBase,
		"Chat":             Chat,
		"AssetsUpload":     AssetsUpload,
		"AssetsList":       AssetsList,
		"AssetsDelete":     AssetsDelete,
		"AssetsDownload":   AssetsDownload,
		"RateLimits":       RateLimits,
		"AcceptTOS":        AcceptTOS,
		"NSFWMgmt":         NSFWMgmt,
		"SetBirth":         SetBirth,
		"MediaPost":        MediaPost,
		"MediaPostLink":    MediaPostLink,
		"VideoUpscale":     VideoUpscale,
		"WSImagine":        WSImagine,
		"WSLiveKit":        WSLiveKit,
		"LiveKitTokens":    LiveKitTokens,
		"ConsoleResponses": ConsoleResponses,
		"ConsoleChat":      ConsoleChat,
	}

	want := map[string]string{
		"Base":             "https://grok.com",
		"AssetsCDN":        "https://assets.grok.com",
		"ConsoleBase":      "https://console.x.ai",
		"Chat":             "https://grok.com/rest/app-chat/conversations/new",
		"AssetsUpload":     "https://grok.com/rest/app-chat/upload-file",
		"AssetsList":       "https://grok.com/rest/assets",
		"AssetsDelete":     "https://grok.com/rest/assets-metadata",
		"AssetsDownload":   "https://assets.grok.com",
		"RateLimits":       "https://grok.com/rest/rate-limits",
		"AcceptTOS":        "https://accounts.x.ai/auth_mgmt.AuthManagement/SetTosAcceptedVersion",
		"NSFWMgmt":         "https://grok.com/auth_mgmt.AuthManagement/UpdateUserFeatureControls",
		"SetBirth":         "https://grok.com/rest/auth/set-birth-date",
		"MediaPost":        "https://grok.com/rest/media/post/create",
		"MediaPostLink":    "https://grok.com/rest/media/post/create-link",
		"VideoUpscale":     "https://grok.com/rest/media/video/upscale",
		"WSImagine":        "wss://grok.com/ws/imagine/listen",
		"WSLiveKit":        "wss://livekit.grok.com",
		"LiveKitTokens":    "https://grok.com/rest/livekit/tokens",
		"ConsoleResponses": "https://console.x.ai/v1/responses",
		"ConsoleChat":      "https://console.x.ai/v1/chat/completions",
	}

	for name, expected := range want {
		if tests[name] != expected {
			t.Fatalf("%s = %q, want %q", name, tests[name], expected)
		}
	}
}

func TestEndpointTableAppliesOverrides(t *testing.T) {
	table := DefaultEndpointTable().WithOverrides(map[string]string{
		"base":          "https://grok.local",
		"assets_cdn":    "https://assets.local",
		"console_base":  "https://console.local",
		"accounts_base": "https://accounts.local",
		"ws_livekit":    "wss://livekit.local",
	})

	if got := table.Resolve("chat"); got != "https://grok.local/rest/app-chat/conversations/new" {
		t.Fatalf("chat endpoint = %q", got)
	}
	if got := table.Resolve("assets_download"); got != "https://assets.local" {
		t.Fatalf("assets download endpoint = %q", got)
	}
	if got := table.Resolve("console_responses"); got != "https://console.local/v1/responses" {
		t.Fatalf("console responses endpoint = %q", got)
	}
	if got := table.Resolve("accept_tos"); got != "https://accounts.local/auth_mgmt.AuthManagement/SetTosAcceptedVersion" {
		t.Fatalf("accept tos endpoint = %q", got)
	}
	if got := table.Resolve("ws_livekit"); got != "wss://livekit.local" {
		t.Fatalf("livekit websocket endpoint = %q", got)
	}
}

func TestEndpointTableBuildsOriginsReferersAndConsoleModelEndpoint(t *testing.T) {
	table := DefaultEndpointTable().WithOverrides(map[string]string{
		"base":         "https://grok.local",
		"assets_cdn":   "https://assets.local",
		"console_base": "https://console.local",
	})

	if got := table.Resolve("base_referer"); got != "https://grok.local/" {
		t.Fatalf("base referer = %q", got)
	}
	if got := table.Resolve("files_referer"); got != "https://grok.local/files" {
		t.Fatalf("files referer = %q", got)
	}
	if got := table.Resolve("imagine_referer"); got != "https://grok.local/imagine" {
		t.Fatalf("imagine referer = %q", got)
	}
	if got := table.Resolve("console_referer"); got != "https://console.local/" {
		t.Fatalf("console referer = %q", got)
	}
	if got := table.Resolve("console_list_models"); got != "https://console.local/auth_mgmt.AuthManagement/ListModels" {
		t.Fatalf("console list models = %q", got)
	}
}

func TestEndpointTableIgnoresUnknownAndEmptyOverrides(t *testing.T) {
	table := DefaultEndpointTable().WithOverrides(map[string]string{
		"chat":    "",
		"unknown": "https://unused.local",
	})

	if got := table.Resolve("chat"); got != Chat {
		t.Fatalf("chat endpoint = %q, want %q", got, Chat)
	}
	if got := table.Resolve("unknown"); got != "" {
		t.Fatalf("unknown endpoint = %q, want empty", got)
	}
}

func TestConfiguredEndpointTableReadsConfigSource(t *testing.T) {
	table := ConfiguredEndpointTable(fakeEndpointConfig{
		"reverse.endpoints.base":            "https://grok.config",
		"reverse.endpoints.console_cluster": "https://cluster.config",
		"reverse.endpoints.chat":            "",
	})

	if got := table.Resolve("chat"); got != "https://grok.config/rest/app-chat/conversations/new" {
		t.Fatalf("chat endpoint = %q", got)
	}
	if got := table.Resolve("console_cluster"); got != "https://cluster.config" {
		t.Fatalf("console cluster endpoint = %q", got)
	}
}

type fakeEndpointConfig map[string]string

func (f fakeEndpointConfig) GetStr(key string, defaultValue string) string {
	if value, ok := f[key]; ok {
		return value
	}
	return defaultValue
}
