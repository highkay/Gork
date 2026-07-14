package model

import "strings"

// BuildModelPrefix 是对外 Build 模型 ID 前缀（例：build/grok-4）。
// 注意：与 registry 中的 grok-build-console（Console 通道）无关。
const BuildModelPrefix = "build/"

// IsBuildModelName 判断客户端模型 ID 是否走 Build 通道命名。
func IsBuildModelName(modelName string) bool {
	return strings.HasPrefix(strings.TrimSpace(modelName), BuildModelPrefix)
}

// UpstreamIDFromBuildModel 从 build/<id> 提取上游模型 id；非法返回空串。
func UpstreamIDFromBuildModel(modelName string) string {
	name := strings.TrimSpace(modelName)
	if !strings.HasPrefix(name, BuildModelPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(name, BuildModelPrefix))
}

// BuildSpecFromName 将 build/<upstream> 解析为独立 CapabilityBuildChat 规格。
// 不查静态 registry，避免污染现有模型名。
func BuildSpecFromName(modelName string) (ModelSpec, bool) {
	upstream := UpstreamIDFromBuildModel(modelName)
	if upstream == "" {
		return ModelSpec{}, false
	}
	return ModelSpec{
		ModelName:  strings.TrimSpace(modelName),
		ModeID:     ModeBuild,
		Tier:       TierBasic,
		Capability: CapabilityBuildChat,
		Enabled:    true,
		PublicName: "Build " + upstream,
	}, true
}
