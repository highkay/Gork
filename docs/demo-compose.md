# Demo Compose

`docker-compose.demo.yml` 只用于公开演示或临时试用环境，不适合作为生产部署模板。它包含自动 reset 服务，会周期性清理指定数据库、Redis DB 和应用本地数据。

## Included Services

| Service | Purpose |
| :-- | :-- |
| `warp-proxy` | Cloudflare WARP 出口代理。 |
| `privoxy` | HTTP proxy，转发到 WARP。 |
| `byparr` | 自动处理 Cloudflare challenge。 |
| `gork` | 主服务。 |
| `demo-reset` | 周期性清理 demo 数据。 |

## Required Hardening Before Any Public Demo

- 替换所有默认密码和默认 DSN。
- 设置强 `GROK_APP_API_KEY` 和 `GROK_APP_APP_KEY`。
- 确认 `POSTGRES_HOST`、`POSTGRES_DB`、`REDIS_HOST`、`REDIS_DB` 只指向 demo 专用资源。
- 不要把生产数据库或生产 Redis 接入 demo compose。
- 对公网 Admin 增加反向代理认证或 IP allowlist。

## Reset Behavior

`demo-reset` 只有在 `DEMO_RESET_CONFIRM=reset-demo-data` 时才会执行清理。启用后会：

- 清理指定 PostgreSQL database 的 `public` schema。
- 对指定 Redis host、port、DB 执行 `FLUSHDB`。
- 清理 gork 应用数据和日志目录。
- 根据 `RESET_INTERVAL_SECONDS` 周期重复执行。
- 当 `RUN_RESET_ON_START=true` 时，启动后立即执行一次 reset。

该服务会使用临时 PostgreSQL 和 Redis helper 容器连接目标网络。清理范围依赖环境变量，配置错误可能删除非 demo 数据。

## Safer Alternatives

- 本地开发使用 `docker-compose.yml`。
- 需要防封能力使用 `docker-compose.warp.yml`。
- 公开演示使用独立云数据库、独立 Redis DB、短期账号和限流反代。
