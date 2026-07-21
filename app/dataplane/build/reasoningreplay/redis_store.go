package reasoningreplay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// RedisStore 多实例共享的推理回放仓储（对齐 chenyme redis ReasoningReplayStore）。
type RedisStore struct {
	client    *redis.Client
	keyPrefix string
}

// NewRedisStore 使用已解析的 go-redis Client；prefix 默认 "gork:reasoning-replay:"。
func NewRedisStore(client *redis.Client, keyPrefix string) *RedisStore {
	if keyPrefix == "" {
		keyPrefix = "gork:reasoning-replay:"
	}
	return &RedisStore{client: client, keyPrefix: keyPrefix}
}

// OpenRedisStore 从 Redis URL 打开客户端并包装。
func OpenRedisStore(rawURL, keyPrefix string) (*RedisStore, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("redis url empty")
	}
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return NewRedisStore(client, keyPrefix), nil
}

func (s *RedisStore) redisKey(model, sessionKey string) string {
	return s.keyPrefix + hashKeyPart(model) + ":" + hashKeyPart(sessionKey)
}

func hashKeyPart(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:16])
}

func (s *RedisStore) Get(ctx context.Context, model, sessionKey string, now time.Time, ttl time.Duration) ([][]byte, bool, error) {
	if s == nil || s.client == nil || model == "" || sessionKey == "" {
		return nil, false, nil
	}
	_ = now
	raw, err := s.client.Get(ctx, s.redisKey(model, sessionKey)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var items [][]byte
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false, err
	}
	if ttl > 0 {
		_ = s.client.Expire(ctx, s.redisKey(model, sessionKey), ttl).Err()
	}
	return cloneReplayItems(items), true, nil
}

func (s *RedisStore) Set(ctx context.Context, model, sessionKey string, items [][]byte, expiresAt time.Time) error {
	if s == nil || s.client == nil || model == "" || sessionKey == "" || len(items) == 0 {
		return nil
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.redisKey(model, sessionKey), raw, ttl).Err()
}

func (s *RedisStore) Delete(ctx context.Context, model, sessionKey string) error {
	if s == nil || s.client == nil || model == "" || sessionKey == "" {
		return nil
	}
	return s.client.Del(ctx, s.redisKey(model, sessionKey)).Err()
}
