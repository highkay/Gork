package openai

import "strings"

func videoCreatePayload(prompt, parentPostID, aspectRatio, resolutionName string, videoLength int, preset string, imageReferences []string) map[string]any {
	videoConfig := map[string]any{
		"parentPostId":   parentPostID,
		"aspectRatio":    aspectRatio,
		"videoLength":    videoLength,
		"resolutionName": resolutionName,
	}
	if len(imageReferences) > 0 {
		videoConfig["isVideoEdit"] = false
		videoConfig["isReferenceToVideo"] = true
		videoConfig["imageReferences"] = imageReferences
	}
	return map[string]any{
		"temporary":        true,
		"modelName":        videoModelName,
		"message":          buildVideoMessage(prompt, preset),
		"enableSideBySide": true,
		"responseMetadata": map[string]any{
			"experiments": []any{},
			"modelConfigOverride": map[string]any{
				"modelMap": map[string]any{
					"videoGenModelConfig": videoConfig,
				},
			},
		},
	}
}

func videoExtendPayload(prompt, parentPostID, extendPostID, aspectRatio, resolutionName string, videoLength int, preset string, startTimeS float64) map[string]any {
	return map[string]any{
		"temporary":        true,
		"modelName":        videoModelName,
		"message":          buildVideoMessage(prompt, preset),
		"enableSideBySide": true,
		"responseMetadata": map[string]any{
			"experiments": []any{},
			"modelConfigOverride": map[string]any{
				"modelMap": map[string]any{
					"videoGenModelConfig": map[string]any{
						"isVideoExtension":        true,
						"videoExtensionStartTime": startTimeS,
						"extendPostId":            extendPostID,
						"stitchWithExtendPostId":  true,
						"originalPrompt":          prompt,
						"originalPostId":          parentPostID,
						"originalRefType":         videoExtensionRefType,
						"mode":                    preset,
						"aspectRatio":             aspectRatio,
						"videoLength":             videoLength,
						"resolutionName":          resolutionName,
						"parentPostId":            parentPostID,
						"isVideoEdit":             false,
					},
				},
			},
		},
	}
}

func buildVideoMessage(prompt, preset string) string {
	flag := videoPresetFlags[preset]
	if flag == "" {
		flag = "--mode=custom"
	}
	return strings.TrimSpace(prompt + " " + flag)
}

func videoExtendStartTime(seconds int) float64 {
	return float64(seconds) + 1.0/24.0
}
