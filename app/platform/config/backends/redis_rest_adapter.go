package backends

import (
	"context"

	"github.com/dslzl/gork/app/platform/deployenv"
	"github.com/dslzl/gork/app/platform/redisrest"
)

type restRedisConfigClient struct {
	client *redisrest.Client
}

type restRedisConfigPipeline struct {
	client      *redisrest.Client
	transaction bool
	commands    [][]any
}

func newRedisRESTConfigBackend(config deployenv.RedisREST) (ConfigBackend, error) {
	client, err := redisrest.New(redisrest.Config{URL: config.URL, Token: config.Token})
	if err != nil {
		return nil, err
	}
	return NewRedisConfigBackend(
		&restRedisConfigClient{client: client},
		RedisConfigOptions{},
	), nil
}

func (c *restRedisConfigClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	result, err := c.client.Do(ctx, "HGETALL", key)
	if err != nil {
		return nil, err
	}
	return redisrest.StringMap(result)
}

func (c *restRedisConfigClient) Get(ctx context.Context, key string) (string, error) {
	result, err := c.client.Do(ctx, "GET", key)
	if err != nil {
		return "", err
	}
	value, _, err := redisrest.String(result)
	return value, err
}

func (c *restRedisConfigClient) Pipeline(transaction bool) RedisPipeline {
	return &restRedisConfigPipeline{client: c.client, transaction: transaction}
}

func (c *restRedisConfigClient) AClose(context.Context) error {
	return nil
}

func (p *restRedisConfigPipeline) Del(_ context.Context, key string) {
	p.commands = append(p.commands, []any{"DEL", key})
}

func (p *restRedisConfigPipeline) HSet(_ context.Context, key string, mapping map[string]string) {
	p.commands = append(p.commands, append([]any{"HSET", key}, redisConfigHashArgs(mapping)...))
}

func (p *restRedisConfigPipeline) Incr(_ context.Context, key string) {
	p.commands = append(p.commands, []any{"INCR", key})
}

func (p *restRedisConfigPipeline) Execute(ctx context.Context) error {
	_, err := p.client.Pipeline(ctx, p.commands, p.transaction)
	return err
}
