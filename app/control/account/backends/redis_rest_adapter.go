package backends

import (
	"context"
	"strconv"

	account "github.com/dslzl/gork/app/control/account"
	"github.com/dslzl/gork/app/platform/deployenv"
	"github.com/dslzl/gork/app/platform/redisrest"
)

type restRedisAccountStore struct {
	client *redisrest.Client
}

func newRedisRESTRepository(config deployenv.RedisREST) (account.AccountRepository, error) {
	store, err := NewRedisRESTAccountStore(config)
	if err != nil {
		return nil, err
	}
	return NewRedisAccountRepository(store), nil
}

func NewRedisRESTAccountStore(config deployenv.RedisREST) (RedisAccountStore, error) {
	client, err := redisrest.New(redisrest.Config{URL: config.URL, Token: config.Token})
	if err != nil {
		return nil, err
	}
	return &restRedisAccountStore{client: client}, nil
}

func (s *restRedisAccountStore) Incr(ctx context.Context, key string) (int, error) {
	result, err := s.client.Do(ctx, "INCR", key)
	if err != nil {
		return 0, err
	}
	return redisrest.Int(result)
}

func (s *restRedisAccountStore) SetNX(ctx context.Context, key string, value string) (bool, error) {
	result, err := s.client.Do(ctx, "SET", key, value, "NX")
	if err != nil {
		return false, err
	}
	return result != nil, nil
}

func (s *restRedisAccountStore) Get(ctx context.Context, key string) (string, bool, error) {
	result, err := s.client.Do(ctx, "GET", key)
	if err != nil {
		return "", false, err
	}
	return redisrest.String(result)
}

func (s *restRedisAccountStore) ScanKeys(ctx context.Context, pattern string) ([]string, error) {
	cursor := "0"
	keys := []string{}
	for {
		result, err := s.client.Do(ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", 100)
		if err != nil {
			return nil, err
		}
		next, batch, err := decodeScanResult(result)
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if next == "0" {
			return keys, nil
		}
		cursor = next
	}
}

func (s *restRedisAccountStore) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	result, err := s.client.Do(ctx, "HGETALL", key)
	if err != nil {
		return nil, err
	}
	return redisrest.StringMap(result)
}

func (s *restRedisAccountStore) HGet(ctx context.Context, key string, field string) (string, bool, error) {
	result, err := s.client.Do(ctx, "HGET", key, field)
	if err != nil {
		return "", false, err
	}
	return redisrest.String(result)
}

func (s *restRedisAccountStore) HSet(ctx context.Context, key string, mapping map[string]string) error {
	args := append([]any{"HSET", key}, redisHashArgs(mapping)...)
	_, err := s.client.Do(ctx, args...)
	return err
}

func (s *restRedisAccountStore) ZAdd(ctx context.Context, key string, members map[string]int) error {
	args := []any{"ZADD", key}
	for member, score := range members {
		args = append(args, score, member)
	}
	_, err := s.client.Do(ctx, args...)
	return err
}

func (s *restRedisAccountStore) ZRangeByScore(
	ctx context.Context,
	key string,
	minExclusive int,
	limit int,
) ([]string, error) {
	args := []any{"ZRANGEBYSCORE", key, "(" + strconv.Itoa(minExclusive), "+inf"}
	if limit > 0 {
		args = append(args, "LIMIT", 0, limit)
	}
	result, err := s.client.Do(ctx, args...)
	if err != nil {
		return nil, err
	}
	return redisrest.StringSlice(result)
}

func (s *restRedisAccountStore) ZRem(ctx context.Context, key string, members ...string) error {
	args := []any{"ZREM", key}
	for _, member := range members {
		args = append(args, member)
	}
	_, err := s.client.Do(ctx, args...)
	return err
}

func (s *restRedisAccountStore) SAdd(ctx context.Context, key string, members ...string) error {
	args := []any{"SADD", key}
	for _, member := range members {
		args = append(args, member)
	}
	_, err := s.client.Do(ctx, args...)
	return err
}

func (s *restRedisAccountStore) SRem(ctx context.Context, key string, members ...string) error {
	args := []any{"SREM", key}
	for _, member := range members {
		args = append(args, member)
	}
	_, err := s.client.Do(ctx, args...)
	return err
}

func (s *restRedisAccountStore) SMembers(ctx context.Context, key string) ([]string, error) {
	result, err := s.client.Do(ctx, "SMEMBERS", key)
	if err != nil {
		return nil, err
	}
	return redisrest.StringSlice(result)
}

func (s *restRedisAccountStore) Close(context.Context) error {
	return nil
}

func decodeScanResult(result any) (string, []string, error) {
	items, ok := result.([]any)
	if !ok || len(items) != 2 {
		return "", nil, strconv.ErrSyntax
	}
	cursor, ok, err := redisrest.String(items[0])
	if err != nil {
		return "", nil, err
	}
	if !ok {
		cursor = "0"
	}
	keys, err := redisrest.StringSlice(items[1])
	return cursor, keys, err
}
