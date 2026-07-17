# Operations Guide

本文覆盖部署、升级、备份、恢复和常见运行时组件选择。

**开发、提交与镜像发布的正确链路**见 [development.md](./development.md)。  
镜像默认由 **GitHub Actions 构建并推送到 GHCR**；运维侧只 pull 指定 `sha-<commit>` 并升级，不要用本机 `docker build/push` 替代正式发布。

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

升级顺序（与 [development.md](./development.md) 一致）：

1. 代码已 push 到部署用远程的 `main`，且 **GitHub Actions 构建成功**。  
2. 在 GHCR Packages 确认存在目标 tag（推荐 `sha-<commit>`）。  
3. 写入 `.env` 的 `GORK_IMAGE`，再 pull + up。

升级标准版：

```bash
# 先固定 .env，例如:
# GORK_IMAGE=ghcr.io/<owner>/gork:sha-<commit>
docker compose pull gork
docker compose up -d
```

本机防封/warpplus 示例：

```bash
# 确认 Actions 已产出 ghcr.io/highkay/gork:sha-<commit> 后再执行
# 编辑 .env → GORK_IMAGE=ghcr.io/highkay/gork:sha-<commit>
docker compose -f docker-compose.warpplus.yml pull gork
docker compose -f docker-compose.warpplus.yml up -d gork
```

回滚镜像：

```bash
# 改回上一成功 tag 后
GORK_IMAGE=ghcr.io/<owner>/gork:sha-<previous> docker compose -f docker-compose.warpplus.yml up -d gork
```

升级前建议：

1. 固定当前镜像 tag 或 digest（写入变更记录）。
2. 备份 `.env`、`data/config.toml`、`data/accounts.db` 或外部数据库。
3. 记录当前 commit/image tag，可从镜像 tag、Compose `.env` 或部署平台版本记录查看。
4. 确认 GitHub Actions 对应 run 为 success，再改 `GORK_IMAGE`。
5. 跑 `/health`、`/meta`、`/v1/models` 和一次最小 chat smoke。

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

## SSO 账号校验与号池清理

console.x.ai 免费账号（SSO cookie）的有效性由两套机制维护：

| 机制 | 入口 | 用途 |
| :-- | :-- | :-- |
| 定时 SSO validation | `account.sso_validation.*` + 进程内 scheduler | 周期性分批探针，写 `ext.sso_validation`，终端失效可软删 |
| 全量 CLI 扫号 | `gork account sso-sweep` | 运维一次性扫 active 账号；仅 session/local 无效走 Admin 删除 |
| 运行时失败 | 请求路径 invalid credentials | 连续失败达 `account.invalid_credentials.max_failures` 后 expire/删除 |

### 探针逻辑（与代码一致）

校验顺序（`app/control/account/sso_validation.go` + `dataplane/reverse`）：

1. **本地预检**（无 HTTP）：空 cookie → `empty`；JWT `exp` 早于 `now-60s` → `jwt_expired`；非 JWT 形态则继续在线探针。
2. **在线会话探针**：对 `accounts.x.ai` 做浏览器式导航，以**最终跳转 URL** 判定会话是否仍被接受（不是只看 JWT 是否未过期）。
3. **Clearance 绑定**：探针 `Acquire` 的 `ClearanceOrigin` 必须是 accounts 端点，不能用 `grok.com` 的 CF cookie 打 accounts（否则会整批 Cloudflare soft-fail）。
4. **Cloudflare / WAF / 限流 / 网络**：单独不删号；scheduler 路径会 **回退 console `ListModels`**，若 ListModels 仍判定凭证死亡则记失败；仅 session 探针路径在 CLI `sso-sweep` 中只把 **session invalid / local invalid** 送去删除。
5. **终端失败**：`session_invalid` / invalid credentials 累计到 `account.sso_validation.max_failures`（默认 3）后 Delete；CF、rate limit、http_block、传输错误只写 soft-fail 标记。

浏览器响应分类（`protocol.ClassifySSOBrowserResponse`）大致为：`ok` / `cloudflare` / `rate_limited` / `session_invalid` / `http_block` / `unknown`。

### 配置

```toml
[account.sso_validation]
enabled = false          # 默认关闭；大规模号池建议按出口与 CF 压力评估后再开
interval_sec = 300
batch_size = 100
concurrency = 10
max_failures = 3
```

对应环境变量：`GROK_ACCOUNT_SSO_VALIDATION_*`（见 [configuration.md](./configuration.md)）。

### CLI：`account sso-sweep`

在**已部署且配置/代理与线上一致**的环境执行（推荐容器内同版本二进制，而不是本机旧 build）：

```bash
# 帮助
docker exec gork /app/gork account sso-sweep --help
# 或本机（需能加载同一 config / 代理）
go run ./cmd/gork account sso-sweep --help

# 只探针、不删除
docker exec gork /app/gork account sso-sweep \
  --dry-run --concurrency 8 --progress-every 100

# 正式清理（通过 Admin API 批量 DELETE /admin/api/tokens）
docker exec gork /app/gork account sso-sweep \
  --concurrency 8 \
  --admin-url http://127.0.0.1:8000 \
  --admin-auth "<app.app_key 或 Admin Bearer>" \
  --delete-batch 100

# 分页/限量（大库分批）
docker exec gork /app/gork account sso-sweep --offset 0 --limit 5000 --dry-run
```

常用 flag：

| Flag | 默认 | 说明 |
| :-- | :-- | :-- |
| `--concurrency` | `8` | 探针并发 |
| `--limit` / `--offset` | `0` / `0` | `limit=0` 表示全部 active |
| `--dry-run` | off | 只探针不删 |
| `--admin-url` | `http://127.0.0.1:8000` | 删除用 Admin 基址 |
| `--admin-auth` | `gork` | Bearer token |
| `--delete-batch` | `100` | 批量删除大小 |
| `--page-size` | `500` | 列举 active 分页 |
| `--progress-every` | `100` | 进度日志间隔 |

输出示例字段：`checked` / `ok` / `session_invalid` / `local_invalid` / `cf` / `rate` / `http_block` / `other` / `deleted`。

仓储一致性（非 SSO 探针）仍用：

```bash
go run ./cmd/gork account check
# 或
docker exec gork /app/gork account check --json
```

### 运维脚本（只读库 / Admin 清理）

容器内 `accounts.db` 常为 root/容器用户权限，宿主机只读打开可能失败；先拷贝再分析：

```bash
docker cp gork:/app/data/accounts.db /tmp/accounts.db

# 号池健康摘要（status / fail reason / quota 等）
python3 scripts/account_health.py /tmp/accounts.db
python3 scripts/account_health.py /tmp/accounts.db --json

# SSO 扫号进度（ext.sso_validation、近期更新速率、粗算 ETA）
python3 scripts/sso_sweep_progress.py /tmp/accounts.db
python3 scripts/sso_sweep_progress.py /tmp/accounts.db --minutes 30

# 仅本地 JWT/空 cookie 预检 + 可选删 expired（无在线探针）
python3 scripts/sso_offline_cleanup.py /tmp/accounts.db --dry-run
python3 scripts/sso_offline_cleanup.py /tmp/accounts.db --auth gork

# 清理 expired；可选禁用 SSO soft-fail 号
python3 scripts/cleanup_bad_accounts.py /tmp/accounts.db --dry-run
python3 scripts/cleanup_bad_accounts.py /tmp/accounts.db --disable-sso-soft-fail

# 403/429 运营分析（配合 config 调参示例）
python3 scripts/analyze_403_429.py --help
# 参考: scripts/config_403_429_tune.example.toml
```

`ext.sso_validation` 常见字段：

| 字段 | 含义 |
| :-- | :-- |
| `last_ok_at` | 最近一次探针成功（毫秒时间戳） |
| `failure_count` | 连续失败次数 |
| `last_fail_stage` | `local` / `session` / `list_models` / `cloudflare` / `rate_limited` / `http_block` / `refresh` 等 |
| `last_fail_reason` | 人类可读原因（日志/脚本里已脱敏 token） |

`last_fail_reason` / `state_reason` 前缀 `sso_validation_*` 表示来自 SSO 校验路径；成功探针会清空这些 stale 标记。

### 进度判断建议

- **几乎扫完**：live active（`status=active` 且 `deleted_at IS NULL`）中带 `sso_validation` 的 ok/fail 覆盖率接近 100%，且无独立 `sso-sweep` 进程、更新速率接近 0。
- **仍在扫**：`sso_sweep_progress.py` 近 N 分钟 `updated` 与 `sso ok/fail` 持续增长；或 CLI 仍在打印 `progress checked=`。
- **漏检**：无 `sso_validation` 且非 soft-deleted 的 active 数量 > 0 → 补跑 `sso-sweep` 或打开 scheduler。
- **不要**把 Cloudflare soft-fail 当成「号已死」批量硬删；先确认代理 clearance 与 accounts 绑定是否正确。

### 安全注意

- 不要把完整 SSO / cookie 贴进 issue、日志汇总或文档。
- `account check` / 脚本输出已尽量脱敏 token；导出 db 副本时同样按密钥处理。
- 全量 `sso-sweep` 会打上游 accounts/console，注意并发、代理容量与 rate limit。
