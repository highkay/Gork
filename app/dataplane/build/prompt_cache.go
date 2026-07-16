package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const promptCacheIdentityVersion = "v1"

// ResolvePromptCacheKey 将客户端显式键或会话种子归一为上游 prompt_cache_key。
// 空种子返回空串（不注入）。与 chenyme #631 对齐：固定长度摘要，降低共享池碰撞。
func ResolvePromptCacheKey(explicitKey, sessionSeed, upstreamModel string) string {
	seed := strings.TrimSpace(explicitKey)
	if seed == "" {
		seed = strings.TrimSpace(sessionSeed)
	}
	model := strings.ToLower(strings.TrimSpace(upstreamModel))
	if seed == "" || model == "" {
		return ""
	}
	source := fmt.Sprintf("gork:prompt-cache:%s:%s:%s", promptCacheIdentityVersion, model, seed)
	digest := sha256.Sum256([]byte(source))
	hexID := hex.EncodeToString(digest[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexID[0:8], hexID[8:12], hexID[12:16], hexID[16:20], hexID[20:32])
}

// PromptCacheKeyFromOverrides 从 OpenAI 请求覆盖字段提取显式缓存键。
func PromptCacheKeyFromOverrides(overrides map[string]any) string {
	if overrides == nil {
		return ""
	}
	for _, key := range []string{"prompt_cache_key", "promptCacheKey"} {
		if v, ok := overrides[key]; ok && v != nil {
			switch typed := v.(type) {
			case string:
				if s := strings.TrimSpace(typed); s != "" {
					return s
				}
			default:
				if s := strings.TrimSpace(fmt.Sprint(typed)); s != "" && s != "<nil>" {
					return s
				}
			}
		}
	}
	return ""
}
