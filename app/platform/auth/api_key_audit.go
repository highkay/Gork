package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
)

// APIKeyMatch 表示一次 API key 鉴权命中结果（不含完整密钥）。
type APIKeyMatch struct {
	// Fingerprint 为 sha256 十六进制前 12 字符，用于计量与日志。
	Fingerprint string
	// Index 为配置列表中的下标（0-based）。
	Index int
}

var (
	apiKeyUseMu    sync.Mutex
	apiKeyUseCount = map[string]*atomic.Int64{}
)

// MatchAPIKey 在 allowed 中查找 token；命中则记录使用计数并返回 Match。
func MatchAPIKey(token string, allowed []string) (APIKeyMatch, bool) {
	for i, key := range allowed {
		if constantTimeStringEqual(token, key) {
			fp := APIKeyFingerprint(key)
			incAPIKeyUse(fp)
			return APIKeyMatch{Fingerprint: fp, Index: i}, true
		}
	}
	return APIKeyMatch{}, false
}

// APIKeyFingerprint 生成稳定、不可逆的短指纹（日志/指标用）。
func APIKeyFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:12]
}

// SnapshotAPIKeyUsage 返回 fingerprint → 使用次数（测试/运维可读）。
func SnapshotAPIKeyUsage() map[string]int64 {
	apiKeyUseMu.Lock()
	defer apiKeyUseMu.Unlock()
	out := make(map[string]int64, len(apiKeyUseCount))
	for fp, counter := range apiKeyUseCount {
		out[fp] = counter.Load()
	}
	return out
}

// ResetAPIKeyUsageForTest 仅测试用。
func ResetAPIKeyUsageForTest() {
	apiKeyUseMu.Lock()
	defer apiKeyUseMu.Unlock()
	apiKeyUseCount = map[string]*atomic.Int64{}
}

func incAPIKeyUse(fingerprint string) {
	apiKeyUseMu.Lock()
	counter, ok := apiKeyUseCount[fingerprint]
	if !ok {
		counter = &atomic.Int64{}
		apiKeyUseCount[fingerprint] = counter
	}
	apiKeyUseMu.Unlock()
	counter.Add(1)
}
