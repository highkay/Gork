package reasoningreplay

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Store 保存无状态多轮所需的上一轮可回放 output items。
// key 边界为 model + sessionKey；sessionKey 应使用已隔离的 PromptCacheKey。
type Store interface {
	Get(ctx context.Context, model, sessionKey string, now time.Time, ttl time.Duration) (items [][]byte, ok bool, err error)
	Set(ctx context.Context, model, sessionKey string, items [][]byte, expiresAt time.Time) error
	Delete(ctx context.Context, model, sessionKey string) error
}

type reasoningReplayEntry struct {
	items     [][]byte
	expiresAt time.Time
	storedAt  time.Time
}

// MemoryStore 单实例有界推理回放缓存。
type MemoryStore struct {
	mu         sync.Mutex
	maxSize    int
	evictBatch int
	values     map[string]reasoningReplayEntry
	ttlSlide   bool
}

// NewMemoryStore 创建内存推理回放仓储；maxSize 为全局条目上限。
func NewMemoryStore(maxSize int) *MemoryStore {
	if maxSize < 1 {
		maxSize = 10240
	}
	evictBatch := maxSize / 80
	if evictBatch < 1 {
		evictBatch = 1
	}
	if evictBatch > 128 {
		evictBatch = 128
	}
	return &MemoryStore{
		maxSize:    maxSize,
		evictBatch: evictBatch,
		values:     make(map[string]reasoningReplayEntry, maxSize),
		ttlSlide:   true,
	}
}

func reasoningReplayMapKey(model, sessionKey string) string {
	return model + "\x00" + sessionKey
}

func cloneReplayItems(items [][]byte) [][]byte {
	if len(items) == 0 {
		return nil
	}
	cloned := make([][]byte, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, append([]byte(nil), item...))
	}
	return cloned
}

func (s *MemoryStore) Get(_ context.Context, model, sessionKey string, now time.Time, ttl time.Duration) ([][]byte, bool, error) {
	if model == "" || sessionKey == "" {
		return nil, false, nil
	}
	key := reasoningReplayMapKey(model, sessionKey)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.values[key]
	if !ok {
		return nil, false, nil
	}
	if !now.Before(entry.expiresAt) {
		delete(s.values, key)
		return nil, false, nil
	}
	if s.ttlSlide {
		if ttl <= 0 {
			ttl = entry.expiresAt.Sub(entry.storedAt)
			if ttl <= 0 {
				ttl = time.Hour
			}
		}
		entry.expiresAt = now.Add(ttl)
		entry.storedAt = now
		s.values[key] = entry
	}
	return cloneReplayItems(entry.items), true, nil
}

func (s *MemoryStore) Set(_ context.Context, model, sessionKey string, items [][]byte, expiresAt time.Time) error {
	if model == "" || sessionKey == "" || len(items) == 0 || expiresAt.IsZero() {
		return nil
	}
	key := reasoningReplayMapKey(model, sessionKey)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.values[key]; !exists {
		s.evictLocked(now)
	}
	s.values[key] = reasoningReplayEntry{items: cloneReplayItems(items), expiresAt: expiresAt, storedAt: now}
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, model, sessionKey string) error {
	if model == "" || sessionKey == "" {
		return nil
	}
	key := reasoningReplayMapKey(model, sessionKey)
	s.mu.Lock()
	delete(s.values, key)
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) evictLocked(now time.Time) {
	for key, entry := range s.values {
		if !now.Before(entry.expiresAt) {
			delete(s.values, key)
		}
	}
	if len(s.values) < s.maxSize {
		return
	}
	type candidate struct {
		key      string
		storedAt time.Time
	}
	candidates := make([]candidate, 0, len(s.values))
	for key, entry := range s.values {
		candidates = append(candidates, candidate{key: key, storedAt: entry.storedAt})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].storedAt.Before(candidates[j].storedAt) })
	count := s.evictBatch
	if count > len(candidates) {
		count = len(candidates)
	}
	for index := 0; index < count; index++ {
		delete(s.values, candidates[index].key)
	}
}
