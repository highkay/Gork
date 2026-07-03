package protocol

import reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"

var (
	MediaPostURL    = reverseruntime.DefaultEndpointTable().Resolve("media_post")
	MediaLinkURL    = reverseruntime.DefaultEndpointTable().Resolve("media_post_link")
	VideoUpscaleURL = reverseruntime.DefaultEndpointTable().Resolve("video_upscale")
)

func MediaPostEndpoint() string {
	return reverseruntime.GlobalEndpointTable().Resolve("media_post")
}

func MediaLinkEndpoint() string {
	return reverseruntime.GlobalEndpointTable().Resolve("media_post_link")
}

func VideoUpscaleEndpoint() string {
	return reverseruntime.GlobalEndpointTable().Resolve("video_upscale")
}

type MediaPostPayloadOptions struct {
	MediaType string
	MediaURL  string
	Prompt    string
}

func BuildMediaPostPayload(options MediaPostPayloadOptions) map[string]any {
	payload := map[string]any{"mediaType": options.MediaType}
	if options.MediaURL != "" {
		payload["mediaUrl"] = options.MediaURL
	}
	if options.Prompt != "" {
		payload["prompt"] = options.Prompt
	}
	return payload
}

func BuildVideoUpscalePayload(videoID string) map[string]any {
	return map[string]any{"videoId": videoID}
}

func BuildMediaLinkPayload(postID string) map[string]any {
	return map[string]any{
		"postId":   postID,
		"source":   "post-page",
		"platform": "web",
	}
}
