package reverse

import (
	controlmodel "github.com/dslzl/gork/app/control/model"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

type transportDefaults struct {
	timeoutS    float64
	contentType string
}

var defaultTransportProfiles = map[TransportKind]transportDefaults{
	TransportKindHTTPSSE: {
		timeoutS:    120.0,
		contentType: "application/json",
	},
	TransportKindHTTPJSON: {
		timeoutS:    30.0,
		contentType: "application/json",
	},
	TransportKindWebSocket: {
		timeoutS:    300.0,
		contentType: "application/json",
	},
	TransportKindGRPCWeb: {
		timeoutS:    15.0,
		contentType: "application/grpc-web+proto",
	},
}

func BuildPlan(spec controlmodel.ModelSpec) ReversePlan {
	endpoint, transportKind := resolveEndpoint(spec)
	defaults, ok := defaultTransportProfiles[transportKind]
	if !ok {
		defaults = defaultTransportProfiles[TransportKindHTTPJSON]
	}
	timeoutS := defaults.timeoutS
	if profile, ok := operationProfileForSpec(spec); ok {
		timeoutS = profile.TimeoutS
	}
	plan := NewReversePlan(endpoint, transportKind, spec.PoolCandidates(), int(spec.ModeID))
	plan.TimeoutS = timeoutS
	plan.ContentType = defaults.contentType
	return plan
}

func resolveEndpoint(spec controlmodel.ModelSpec) (string, TransportKind) {
	endpoints := reverseruntime.GlobalEndpointTable()
	if spec.IsChat() {
		return endpoints.Resolve("chat"), TransportKindHTTPSSE
	}
	if spec.IsImage() {
		return endpoints.Resolve("ws_imagine"), TransportKindWebSocket
	}
	if spec.IsImageEdit() {
		return endpoints.Resolve("chat"), TransportKindHTTPSSE
	}
	if spec.IsVideo() {
		return endpoints.Resolve("media_post"), TransportKindHTTPJSON
	}
	if spec.IsVoice() {
		return endpoints.Resolve("chat"), TransportKindHTTPSSE
	}
	return endpoints.Resolve("chat"), TransportKindHTTPSSE
}

func operationProfileForSpec(spec controlmodel.ModelSpec) (reverseruntime.OperationProfile, bool) {
	if spec.IsChat() {
		return reverseruntime.Profiles["chat"], true
	}
	if spec.IsImage() {
		return reverseruntime.Profiles["image"], true
	}
	if spec.IsImageEdit() {
		return reverseruntime.Profiles["image_edit"], true
	}
	if spec.IsVideo() {
		return reverseruntime.Profiles["video"], true
	}
	if spec.IsVoice() {
		return reverseruntime.Profiles["voice"], true
	}
	return reverseruntime.OperationProfile{}, false
}
