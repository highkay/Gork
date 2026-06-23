package protocol

import (
	"encoding/json"
	"net/url"
	"strings"

	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

var (
	LiveKitTokenURL = reverseruntime.DefaultEndpointTable().Resolve("livekit_tokens")
	LiveKitWSBase   = reverseruntime.DefaultEndpointTable().Resolve("ws_livekit")
)

type LiveKitTokenOptions struct {
	Voice             string
	Personality       string
	Speed             float64
	CustomInstruction string
	VoiceSet          bool
	PersonalitySet    bool
	SpeedSet          bool
}

func LiveKitTokenEndpoint() string {
	return reverseruntime.GlobalEndpointTable().Resolve("livekit_tokens")
}

func LiveKitWebSocketBase() string {
	return reverseruntime.GlobalEndpointTable().Resolve("ws_livekit")
}

func BuildLiveKitTokenRequestPayload(options LiveKitTokenOptions) []byte {
	voice := options.Voice
	if !options.VoiceSet && voice == "" {
		voice = "ara"
	}
	personality := options.Personality
	if !options.PersonalitySet && personality == "" {
		personality = "assistant"
	}
	speed := options.Speed
	if !options.SpeedSet && speed == 0 {
		speed = 1.0
	}
	session := map[string]any{
		"voice":          voice,
		"personality":    nil,
		"playback_speed": speed,
		"enable_vision":  false,
		"turn_detection": map[string]any{"type": "server_vad"},
	}
	if options.CustomInstruction != "" {
		session["instructions"] = options.CustomInstruction
		session["is_raw_instructions"] = true
	} else {
		session["personality"] = personality
	}
	sessionBody, _ := json.Marshal(session)
	outer := map[string]any{
		"sessionPayload":       string(sessionBody),
		"requestAgentDispatch": false,
		"livekitUrl":           LiveKitWebSocketBase(),
		"params":               map[string]any{"enable_markdown_transcript": "true"},
	}
	body, _ := json.Marshal(outer)
	return body
}

func BuildLiveKitWSURL(accessToken string) string {
	base := strings.TrimRight(LiveKitWebSocketBase(), "/")
	rtc := base + "/rtc"
	if strings.HasSuffix(base, "/rtc") {
		rtc = base
	}
	return rtc + "?auto_subscribe=1&sdk=js&version=2.11.4&protocol=15&access_token=" + url.QueryEscape(accessToken)
}
