package runtime

import controlmodel "github.com/dslzl/gork/app/control/model"

type OperationProfile struct {
	TimeoutS     float64
	MaxRetries   int
	RetryCodes   []int
	RetryDelayS  float64
	IdleTimeoutS float64
	ProxyScope   string
	Capability   controlmodel.Capability
	FeedbackKind string
}

func DefaultOperationProfile() OperationProfile {
	return OperationProfile{
		TimeoutS:    30.0,
		RetryDelayS: 1.0,
	}
}

func (p OperationProfile) RetriesStatus(statusCode int) bool {
	for _, code := range p.RetryCodes {
		if code == statusCode {
			return true
		}
	}
	return false
}

var (
	ChatProfile = OperationProfile{
		TimeoutS:     120.0,
		MaxRetries:   1,
		RetryCodes:   []int{502, 503},
		RetryDelayS:  2.0,
		IdleTimeoutS: 30.0,
		ProxyScope:   "console",
		Capability:   controlmodel.CapabilityChat,
		FeedbackKind: "chat",
	}
	ImageProfile = OperationProfile{
		TimeoutS:     300.0,
		RetryDelayS:  1.0,
		IdleTimeoutS: 60.0,
		ProxyScope:   "console",
		Capability:   controlmodel.CapabilityImage,
		FeedbackKind: "image",
	}
	ImageEditProfile = OperationProfile{
		TimeoutS:     120.0,
		MaxRetries:   1,
		RetryCodes:   []int{502, 503},
		RetryDelayS:  2.0,
		IdleTimeoutS: 30.0,
		ProxyScope:   "console",
		Capability:   controlmodel.CapabilityImageEdit,
		FeedbackKind: "image_edit",
	}
	VideoProfile = OperationProfile{
		TimeoutS:     60.0,
		MaxRetries:   1,
		RetryCodes:   []int{429, 502, 503},
		RetryDelayS:  5.0,
		ProxyScope:   "console",
		Capability:   controlmodel.CapabilityVideo,
		FeedbackKind: "video",
	}
	VoiceProfile = OperationProfile{
		TimeoutS:     120.0,
		RetryDelayS:  1.0,
		IdleTimeoutS: 15.0,
		ProxyScope:   "console",
		Capability:   controlmodel.CapabilityVoice,
		FeedbackKind: "voice",
	}
	AssetProfile = OperationProfile{
		TimeoutS:     60.0,
		MaxRetries:   2,
		RetryCodes:   []int{502, 503},
		RetryDelayS:  1.0,
		ProxyScope:   "console",
		Capability:   controlmodel.CapabilityAsset,
		FeedbackKind: "asset",
	}
	GRPCProfile = OperationProfile{
		TimeoutS:     15.0,
		MaxRetries:   1,
		RetryCodes:   []int{503},
		RetryDelayS:  0.5,
		ProxyScope:   "grpc",
		FeedbackKind: "transport",
	}
	NSFWProfile = OperationProfile{
		TimeoutS:     120.0,
		MaxRetries:   1,
		RetryCodes:   []int{502, 503},
		RetryDelayS:  1.0,
		IdleTimeoutS: 30.0,
		ProxyScope:   "console",
		FeedbackKind: "nsfw",
	}
	LiveKitProfile = OperationProfile{
		TimeoutS:     15.0,
		MaxRetries:   1,
		RetryCodes:   []int{502, 503},
		RetryDelayS:  1.0,
		ProxyScope:   "console",
		FeedbackKind: "livekit",
	}
)

var Profiles = map[string]OperationProfile{
	"chat":       ChatProfile,
	"image":      ImageProfile,
	"image_edit": ImageEditProfile,
	"video":      VideoProfile,
	"voice":      VoiceProfile,
	"asset":      AssetProfile,
	"grpc":       GRPCProfile,
	"nsfw":       NSFWProfile,
	"livekit":    LiveKitProfile,
}
