package reverse

import (
	controlmodel "github.com/dslzl/gork/app/control/model"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

type BuildPlanOptions struct {
	Request map[string]any
}

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

func BuildPlan(spec controlmodel.ModelSpec, options ...BuildPlanOptions) ReversePlan {
	request := map[string]any{}
	if len(options) > 0 && options[0].Request != nil {
		request = options[0].Request
	}
	endpoint, transportKind := resolveEndpoint(spec, request)
	defaults, ok := defaultTransportProfiles[transportKind]
	if !ok {
		defaults = defaultTransportProfiles[TransportKindHTTPJSON]
	}
	plan := NewReversePlan(endpoint, transportKind, spec.PoolCandidates(), int(spec.ModeID))
	plan.TimeoutS = defaults.timeoutS
	plan.ContentType = defaults.contentType
	return plan
}

func resolveEndpoint(spec controlmodel.ModelSpec, _ map[string]any) (string, TransportKind) {
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
