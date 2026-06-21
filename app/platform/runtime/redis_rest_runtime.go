package runtime

import (
	"context"

	"github.com/dslzl/gork/app/platform/deployenv"
	"github.com/dslzl/gork/app/platform/redisrest"
)

type restRedisRuntimeClient struct {
	client *redisrest.Client
}

func newRedisRESTRuntimeClient(config deployenv.RedisREST) (RedisRuntimeClient, error) {
	client, err := redisrest.New(redisrest.Config{URL: config.URL, Token: config.Token})
	if err != nil {
		return nil, err
	}
	return &restRedisRuntimeClient{client: client}, nil
}

func (c *restRedisRuntimeClient) Get(ctx context.Context, key string) (any, error) {
	return c.client.Do(ctx, "GET", key)
}

func (c *restRedisRuntimeClient) SetNX(ctx context.Context, key, value string, ttlMS int) (bool, error) {
	result, err := c.client.Do(ctx, "SET", key, value, "NX", "PX", maxRuntimeInt(1, ttlMS))
	if err != nil {
		return false, err
	}
	return result != nil, nil
}

func (c *restRedisRuntimeClient) Expire(ctx context.Context, key string, ttlSeconds int) error {
	_, err := c.client.Do(ctx, "EXPIRE", key, maxRuntimeInt(1, ttlSeconds))
	return err
}

func (c *restRedisRuntimeClient) Delete(ctx context.Context, key string) error {
	_, err := c.client.Do(ctx, "DEL", key)
	return err
}

func (c *restRedisRuntimeClient) HSet(ctx context.Context, key string, mapping map[string]string) error {
	args := append([]any{"HSET", key}, runtimeRedisHashArgs(mapping)...)
	_, err := c.client.Do(ctx, args...)
	return err
}

func (c *restRedisRuntimeClient) HGetAll(ctx context.Context, key string) (map[string]any, error) {
	values, err := c.client.Do(ctx, "HGETALL", key)
	if err != nil {
		return nil, err
	}
	raw, err := redisrest.StringMap(values)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any, len(raw))
	for field, value := range raw {
		result[field] = value
	}
	return result, nil
}

func (c *restRedisRuntimeClient) AClose(context.Context) error {
	return nil
}
