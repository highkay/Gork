package model

import (
	"fmt"
	"sync"
)

var Models = []ModelSpec{
	{ModelName: "grok-4.20-0309-non-reasoning", ModeID: ModeFast, Tier: TierBasic, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Non-Reasoning"},
	{ModelName: "grok-4.20-0309", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309"},
	{ModelName: "grok-4.20-0309-reasoning", ModeID: ModeExpert, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Reasoning"},
	{ModelName: "grok-4.20-0309-non-reasoning-super", ModeID: ModeFast, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Non-Reasoning Super"},
	{ModelName: "grok-4.20-0309-super", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Super"},
	{ModelName: "grok-4.20-0309-reasoning-super", ModeID: ModeExpert, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Reasoning Super"},
	{ModelName: "grok-4.20-0309-non-reasoning-heavy", ModeID: ModeFast, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Non-Reasoning Heavy"},
	{ModelName: "grok-4.20-0309-heavy", ModeID: ModeAuto, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Heavy"},
	{ModelName: "grok-4.20-0309-reasoning-heavy", ModeID: ModeExpert, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Reasoning Heavy"},
	{ModelName: "grok-4.20-multi-agent-0309", ModeID: ModeHeavy, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent 0309"},
	{ModelName: "grok-4.20-fast", ModeID: ModeFast, Tier: TierBasic, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Fast", PreferBest: true},
	{ModelName: "grok-4.20-auto", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Auto", PreferBest: true},
	{ModelName: "grok-4.20-expert", ModeID: ModeExpert, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Expert", PreferBest: true},
	{ModelName: "grok-4.20-heavy", ModeID: ModeHeavy, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Heavy", PreferBest: true},
	{ModelName: "grok-4.3-fast", ModeID: ModeFast, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 Fast", PreferBest: true},
	{ModelName: "grok-4.3-beta", ModeID: ModeGrok43, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.3 Beta"},
	{ModelName: "grok-imagine-image-lite", ModeID: ModeFast, Tier: TierBasic, Capability: CapabilityImage, Enabled: true, PublicName: "Grok Imagine Image Lite"},
	{ModelName: "grok-imagine-image", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityImage, Enabled: true, PublicName: "Grok Imagine Image"},
	{ModelName: "grok-imagine-image-pro", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityImage, Enabled: true, PublicName: "Grok Imagine Image Pro"},
	{ModelName: "grok-imagine-image-edit", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityImageEdit, Enabled: true, PublicName: "Grok Imagine Image Edit"},
	{ModelName: "grok-imagine-video", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityVideo, Enabled: true, PublicName: "Grok Imagine Video"},
	{ModelName: "grok-4.3-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 (Console)"},
	{ModelName: "grok-4.3-low", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 Low Thinking"},
	{ModelName: "grok-4.3-medium", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 Medium Thinking"},
	{ModelName: "grok-4.3-high", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 High Thinking"},
	{ModelName: "grok-4.20-0309-reasoning-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 0309 Reasoning (Console)"},
	{ModelName: "grok-4.20-0309-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 0309 (Console)"},
	{ModelName: "grok-4.20-multi-agent-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent (Console)"},
	{ModelName: "grok-4.20-multi-agent-low", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent Low"},
	{ModelName: "grok-4.20-multi-agent-medium", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent Medium"},
	{ModelName: "grok-4.20-multi-agent-high", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent High"},
	{ModelName: "grok-4.20-multi-agent-xhigh", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent XHigh"},
	{ModelName: "grok-4.20-0309-non-reasoning-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 0309 Non-Reasoning (Console)"},
	{ModelName: "grok-build-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok Build 0.1 (Console)"},
}

var (
	modelsByName       = buildModelsByName()
	modelsByCapability = buildModelsByCapability()
	dynamicProviderMu  sync.RWMutex
	dynamicProvider    func() []ModelSpec
)

func buildModelsByName() map[string]ModelSpec {
	byName := make(map[string]ModelSpec, len(Models))
	for _, spec := range Models {
		byName[spec.ModelName] = spec
	}
	return byName
}

func buildModelsByCapability() map[int][]ModelSpec {
	byCapability := make(map[int][]ModelSpec)
	for _, spec := range Models {
		key := int(spec.Capability)
		byCapability[key] = append(byCapability[key], spec)
	}
	return byCapability
}

func Get(modelName string) (ModelSpec, bool) {
	if spec, ok := modelsByName[modelName]; ok {
		return spec, true
	}
	for _, spec := range dynamicModels() {
		if spec.ModelName == modelName {
			return spec, true
		}
	}
	return ModelSpec{}, false
}

func Resolve(modelName string) (ModelSpec, error) {
	spec, ok := Get(modelName)
	if !ok {
		if buildSpec, buildOK := BuildSpecFromName(modelName); buildOK {
			return buildSpec, nil
		}
		return ModelSpec{}, fmt.Errorf("Unknown model: '%s'", modelName)
	}
	return spec, nil
}

func ListEnabled() []ModelSpec {
	all := allModels()
	enabled := make([]ModelSpec, 0, len(all))
	for _, spec := range all {
		if spec.Enabled {
			enabled = append(enabled, spec)
		}
	}
	return enabled
}

func ListByCapability(cap Capability) []ModelSpec {
	matches := make([]ModelSpec, 0, len(modelsByCapability[int(cap)]))
	for _, spec := range allModels() {
		if spec.Enabled && spec.Capability&cap != 0 {
			matches = append(matches, spec)
		}
	}
	return matches
}

func SetDynamicProvider(provider func() []ModelSpec) func() {
	dynamicProviderMu.Lock()
	previous := dynamicProvider
	dynamicProvider = provider
	dynamicProviderMu.Unlock()
	return func() {
		dynamicProviderMu.Lock()
		dynamicProvider = previous
		dynamicProviderMu.Unlock()
	}
}

func allModels() []ModelSpec {
	all := append([]ModelSpec{}, Models...)
	seen := make(map[string]struct{}, len(all))
	for _, spec := range all {
		seen[spec.ModelName] = struct{}{}
	}
	for _, spec := range dynamicModels() {
		if spec.ModelName == "" {
			continue
		}
		if _, ok := seen[spec.ModelName]; ok {
			continue
		}
		seen[spec.ModelName] = struct{}{}
		all = append(all, spec)
	}
	return all
}

func dynamicModels() (models []ModelSpec) {
	dynamicProviderMu.RLock()
	provider := dynamicProvider
	dynamicProviderMu.RUnlock()
	if provider == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			models = nil
		}
	}()
	return provider()
}
