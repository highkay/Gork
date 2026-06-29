# Operations Guide

本文覆盖部署、升级、备份、恢复和常见运行时组件选择。

## Docker and Compose

标准版优先使用：

```bash
cp .env.example .env
docker compose up -d
docker compose logs -f gork
```

标准版只启动 gork，适合出口 IP 干净、没有 Cloudflare challenge 的环境。

防封版使用：

```bash
docker compose -f docker-compose.warp.yml up -d
```

防封版额外启动 MicroWARP、Privoxy、Byparr，适合出口 IP 被 Cloudflare 风控或需要自动刷新 clearance 的环境。防封版复杂度更高，应在标准版出现 403 或 challenge 后再启用；FlareSolverr 可作为显式 fallback 手动启用。

## Redis Runtime vs Redis Account Storage

| 项 | 环境变量 | 用途 | 是否保存账号 |
| :-- | :-- | :-- | :-- |
| Redis account storage | `ACCOUNT_STORAGE=redis` + `ACCOUNT_REDIS_URL` | 保存账号、状态、配额、分页索引 | 是 |
| Redis runtime | `RUNTIME_REDIS_URL` | 保存后台任务快照、scheduler leader lock、运行时协调 | 否 |

两者可以共用同一个 Redis，但语义不同。生产环境建议：

- 单实例部署可先不启用 Redis runtime。
- 多副本部署应启用 Redis runtime，避免后台任务和调度锁只存在单进程内存。
- Redis account storage 适合无本地磁盘持久化或多实例共享账号池的部署。

## SQL Backends

账号存储支持 MySQL 和 PostgreSQL。示例：

```env
ACCOUNT_STORAGE=mysql
ACCOUNT_MYSQL_URL=user:password@tcp(db.example.com:3306)/gork?parseTime=true&tls=true

ACCOUNT_STORAGE=postgresql
ACCOUNT_POSTGRESQL_URL=postgres://user:password@db.example.com:5432/gork?sslmode=require
```

云数据库常见参数：

- MySQL 使用 `tls=true` 或驱动支持的自定义 TLS 配置。
- PostgreSQL 使用 `sslmode=require`、`verify-full`，并按云厂商要求提供 CA。
- 连接池变量为 `ACCOUNT_SQL_POOL_SIZE`、`ACCOUNT_SQL_MAX_OVERFLOW`、`ACCOUNT_SQL_POOL_TIMEOUT`、`ACCOUNT_SQL_POOL_RECYCLE`。

## Logs and Observability

- `LOG_LEVEL` 控制控制台日志等级。
- `LOG_FILE_ENABLED=true` 会写入本地日志目录。
- `observability.metrics_enabled=true` 后开放 `/metrics`。
- `observability.pprof_enabled=true` 后开放 `/debug/pprof/*`，仅建议临时启用。
- `/admin/api/status` 可查看 runtime、scheduler、proxy clearance、动态模型、media cache 和最近上游错误摘要。

## Upgrade and Rollback

升级标准版：

```bash
docker compose pull gork
docker compose up -d
```

回滚镜像：

```bash
GORK_IMAGE=ghcr.io/dslzl/gork:<previous-tag> docker compose up -d
```

升级前建议：

1. 固定当前镜像 tag 或 digest。
2. 备份 `.env`、`data/config.toml`、`data/accounts.db` 或外部数据库。
3. 记录当前 commit/image tag，可从镜像 tag、Compose `.env` 或部署平台版本记录查看。
4. 跑 `/health`、`/meta`、`/v1/models` 和一次最小 chat smoke。

## Backup and Restore

本地模式：

- 备份 `data/config.toml`、`data/accounts.db`、必要的 `data/media` 和日志归档。
- 恢复时先停容器，替换文件，再启动。

Redis 模式：

- 备份 Redis RDB/AOF 或云厂商快照。
- 恢复前确认 DB 编号和 key 前缀没有指向生产共享实例。

SQL 模式：

- 用 `mysqldump`、`pg_dump` 或云厂商快照备份账号表和 schema version 表。
- 回滚代码前先确认新版本是否执行过 schema 迁移；如已迁移，应使用同一时间点的数据快照回滚。

## Go Main and Python Mirror

本仓库 Go 主线和 Python mirror 分支职责不同：

- `main` 保持 Go 主线实现。
- `python` mirror 用于跟踪上游 Python 变化。
- 端口 Python 上游变更时，先阅读 `.codex/skills/port-python-upstream/SKILL.md`。
- 不要把 Python mirror 文件作为普通功能改动混入 Go 主线提交。

## Version and Update Check

- `/meta` 暴露当前服务元信息。
- `/meta/update` 会检查 GitHub release，并缓存结果。
- 更新检查失败不会影响主服务请求。
- 如需完全固定版本，生产部署使用镜像 tag、`sha-<commit>` tag 或 digest。
