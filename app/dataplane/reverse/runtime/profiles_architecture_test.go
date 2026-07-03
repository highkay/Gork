package runtime

import (
	"testing"

	controlmodel "github.com/dslzl/gork/app/control/model"
)

func TestOperationProfilesCoverProductOperations(t *testing.T) {
	// "grpc" is transport-only for auth/setup helpers, not a product operation.
	required := []string{"chat", "image", "image_edit", "video", "voice", "asset", "nsfw", "livekit"}
	for _, name := range required {
		profile, ok := Profiles[name]
		if !ok {
			t.Fatalf("Profiles missing %q", name)
		}
		if profile.TimeoutS <= 0 {
			t.Fatalf("%s TimeoutS = %v", name, profile.TimeoutS)
		}
		if profile.ProxyScope == "" {
			t.Fatalf("%s ProxyScope is empty", name)
		}
	}
}

func TestOperationProfilesCarryCapabilityAndFeedbackPolicy(t *testing.T) {
	cases := map[string]controlmodel.Capability{
		"chat":       controlmodel.CapabilityChat,
		"image":      controlmodel.CapabilityImage,
		"image_edit": controlmodel.CapabilityImageEdit,
		"video":      controlmodel.CapabilityVideo,
		"voice":      controlmodel.CapabilityVoice,
		"asset":      controlmodel.CapabilityAsset,
	}
	for name, capability := range cases {
		profile := Profiles[name]
		if profile.Capability != capability {
			t.Fatalf("%s Capability = %v, want %v", name, profile.Capability, capability)
		}
		if profile.FeedbackKind == "" {
			t.Fatalf("%s FeedbackKind is empty", name)
		}
	}
}
