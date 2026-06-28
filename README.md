<img width="2172" height="724" alt="7d8bbec891a9885f567a422de442c2b7" src="https://github.com/user-attachments/assets/a39011c4-fa80-4d89-a84c-498e3f9ea32b" />

[![Go](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![OpenAI Compatible](https://img.shields.io/badge/API-OpenAI%20compatible-111827)](#api-%E7%AB%AF%E7%82%B9)
[![License](https://img.shields.io/badge/license-MIT-16a34a)](LICENSE)

> [!NOTE]
> 本项目仅供学习与研究交流。请务必遵守 Grok 的使用条款及当地法律法规，不得用于非法用途。二开与 PR 请保留原作者与前端标识。

<br>

Gork 是一个基于 **Go** 构建的 Grok 网关，将 Grok Web 能力以 OpenAI 兼容 API 的方式对外提供。核心特性：

- OpenAI 兼容接口：`/v1/models`、`/v1/chat/completions`、`/v1/responses`、`/v1/images/generations`、`/v1/images/edits`、`/v1/videos`、`/v1/videos/{video_id}`、`/v1/videos/{video_id}/content`
- Anthropic 兼容接口：`/v1/messages`
- 支持流式与非流式对话、显式思考输出、函数工具结构透传，统一的 token / usage 统计
- 支持多账号池、层级选号、失败反馈、额度同步与自动维护
- 支持本地缓存图片、视频与本地代理链接返回
- 支持文生图、图像编辑、文生视频、图生视频
- 内置 Admin 后台管理、Web Chat、Masonry 生图、ChatKit 语音页面
- 支持 `console.x.ai` 免费账号，新增 `*-console` 模型系列
- 已修复 `grok.com` 路由常见 403 问题，内置 `x-statsig-id` 兼容修复，普通场景下无需额外浏览器签名服务
- 支持大批量令牌服务端分页、后台导入任务，以及可选 Redis 运行时协调

<br>

## 文档导航

| 文档 | 内容 |
| :-- | :-- |
| [Architecture](docs/architecture.md) | 模块分层、请求流和依赖边界 |
| [Configuration](docs/configuration.md) | 由配置 schema 生成的完整 TOML / `GROK_` 环境变量表 |
| [API Compatibility](docs/api-compatibility.md) | OpenAI、Anthropic、Admin/WebUI 私有 API 兼容程度和限制 |
| [Security](docs/security.md) | 密钥、鉴权、CORS、媒体 URL、日志脱敏、TLS 和容器加固 |
| [Operations](docs/operations.md) | Docker、Compose、防封版、Redis、SQL、日志、升级、备份、恢复 |
| [Troubleshooting](docs/troubleshooting.md) | 401、403、429、5xx、Cloudflare、LiveKit/WebSocket、asset upload 排查 |
| [Demo Compose](docs/demo-compose.md) | `docker-compose.demo.yml` 的 demo 限定用途和 reset 行为 |
| [English README](docs/README.en.md) | 英文快速入口 |

<br>

## 镜像说明

本仓库基于上游 [chenyme/grok2api](https://github.com/chenyme/grok2api) 二次构建的仓库 [jiujiu532/grok2api](https://github.com/jiujiu532/grok2api)三开，提供预编译的 Docker 镜像：

### gork 主镜像

| 项 | 值 |
| :-- | :-- |
| 默认镜像 | `ghcr.io/dslzl/gork:latest`（生产建议改用版本、sha 或 digest） |
| 架构 | `linux/amd64`, `linux/arm64` |
| 基础镜像 | Go 静态二进制运行镜像 |
| 默认端口 | `8000` |
| 默认数据目录 | `/app/data` |
| 默认日志目录 | `/app/logs` |

镜像发布会同时生成 `latest`、分支 tag、语义化版本 tag 和 `sha-<commit>` tag，并附带 SBOM 与 provenance attestation。生产部署建议在 `.env` 中设置 `GORK_IMAGE=ghcr.io/dslzl/gork:<version|sha-tag>`，需要完全固定供应链时可改为 `GORK_IMAGE=ghcr.io/dslzl/gork@sha256:<digest>`。

### privoxy-warp 镜像（防封版专用）

| 项 | 值 |
| :-- | :-- |
| 默认镜像 | Compose 默认固定到 digest，可通过 `PRIVOXY_WARP_IMAGE` 覆盖 |
| 架构 | `linux/amd64`, `linux/arm64` |
| 说明 | 预配置好 WARP SOCKS5 转发规则的 Privoxy，与 `caomingjun/warp` 配合使用 |

Compose 示例中的第三方镜像默认 pin 到 digest，并保留 `WARP_IMAGE`、`PRIVOXY_WARP_IMAGE`、`FLARESOLVERR_IMAGE`、`REDIS_IMAGE` 等环境变量用于显式升级。

<br>

## 快速开始

本项目提供两种部署方式，按需选择：

| 方式 | 说明 | 适用场景 |
| :-- | :-- | :-- |
| **标准版** | 仅 gork，直连 Grok | IP 干净、无 Cloudflare 拦截问题 |
| **防封版** | gork + WARP + Privoxy + FlareSolverr | IP 被 Cloudflare 拦截、需要稳定访问 |

决策树：

1. 先部署标准版并完成 `/health`、`/v1/models` 和一次最小 chat smoke。
2. 如果标准版可用，保持标准版；它的组件少、排障成本低、升级风险小。
3. 如果频繁出现 403、Cloudflare challenge、clearance 失效或代理出口不可控，再切换防封版。
4. 如果只需要自带 Redis，不需要 WARP/FlareSolverr，使用标准版 Compose 的 Redis profile，不要上防封版。

> [!TIP]
> 当前版本已内置针对 `grok.com` 常见 403 问题的兼容修复，标准版可直接部署验证，无需额外浏览器签名服务。
> 如果仍然出现 403，通常与出口 IP 被 Cloudflare 风控、`cf_clearance` 失效或代理环境有关，此时建议切换到防封版部署。

### 方式一：标准版（Docker Compose）

```bash
git clone https://github.com/dslzl/gork
cd gork
cp .env.example .env
docker compose up -d
```

查看日志：

```bash
docker compose logs -f gork
```

> 使用 `docker-compose.yml`，仅启动 gork 容器，代理配置默认为空（直连）。

Compose 默认开启 `read_only: true` 与 `no-new-privileges`。容器内只有 `/app/data`、`/app/logs` 可写，临时文件写入 `/app/data/tmp`；如果自定义挂载目录，需要确保这两个目录对容器内运行用户可写。

### 方式二：防封版（WARP + FlareSolverr 一键部署）

> **前置要求**：服务器需支持 `NET_ADMIN` + `SYS_MODULE` 权限（KVM/XEN 虚拟化均支持，OpenVZ/LXC 不支持）。

```bash
git clone https://github.com/dslzl/gork
cd gork
docker compose -f docker-compose.warp.yml up -d
```

启动后可运行 smoke check，确认 gork 健康检查可用，并通过本机 Privoxy 出口访问 `grok.com`：

```bash
sh scripts/smoke_warp_clearance.sh
```

脚本默认使用 `docker-compose.warp.yml`、`http://127.0.0.1:40080` 和 `https://grok.com`，可通过 `COMPOSE_FILE`、`PROXY_URL`、`TARGET_URL` 覆盖。

防封版会自动启动以下服务并完成配置：

| 服务 | 说明 |
| :-- | :-- |
| `warp-proxy` | Cloudflare WARP 出口代理，提供干净的 Cloudflare IP |
| `privoxy` | HTTP 代理，将流量转发到 WARP（已预配置，无需手动操作） |
| `flaresolverr` | 自动解 Cloudflare 挑战，获取 cf_clearance |
| `gork` | 主服务，代理配置由 init 容器自动写入 |

启动后代理配置已自动完成，进入 Admin 后台添加账号即可使用。

### 方式三：Docker 单容器

```bash
docker run -d \
  --name gork \
  -p 8000:8000 \
  -e TZ=Asia/Shanghai \
  -e LOG_LEVEL=INFO \
  -e ACCOUNT_STORAGE=local \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/logs:/app/logs \
  --restart unless-stopped \
  ghcr.io/dslzl/gork:<version-or-sha-tag>
```

Windows PowerShell：

```powershell
docker run -d `
  --name gork `
  -p 8000:8000 `
  -e TZ=Asia/Shanghai `
  -e LOG_LEVEL=INFO `
  -e ACCOUNT_STORAGE=local `
  -v ${PWD}/data:/app/data `
  -v ${PWD}/logs:/app/logs `
  --restart unless-stopped `
  ghcr.io/dslzl/gork:<version-or-sha-tag>
```

### 方式四：本地源码部署

前置：Go 1.25+。Python 3.13+ 与 `uv` 仅用于迁移期回归测试。

```bash
git clone https://github.com/dslzl/gork
cd gork
cp .env.example .env
go run ./cmd/gork

# 可选：构建本地二进制
go build -o gork ./cmd/gork
./gork
```

### 首次启动

服务首次启动时会为 `app.app_key` 写入固定初始 Admin key `gork` 并输出到日志。访问 `http://localhost:8000/admin/login`，使用日志中的初始 key 登录后依次完成：

1. 修改 `app.app_key`（Admin 后台登录密码）
2. 设置 `app.api_key`（API 调用鉴权密钥，留空则不鉴权）
3. 设置 `app.app_url`（公网地址，否则图片、视频链接会 403）

> 默认配置源是仓库内的 `config.defaults.toml`；`config.example.toml` 仅作样例。运行时配置写入 `${DATA_DIR}/config.toml`，保存后即时生效，无需重启容器。
> Admin 保存配置会规范化 TOML 并移除手写注释；需要长期保留的说明请写在 `config.example.toml` 或外部文档中。

<br>

## 升级与回滚

无论标准版还是防封版，升级时只需要更新 `gork` 主镜像即可。WARP、Privoxy、FlareSolverr 等防封组件基本不需要更新。

### 标准版升级

```bash
GORK_IMAGE=ghcr.io/dslzl/gork:<version-or-sha-tag>
docker pull "$GORK_IMAGE"
docker compose up -d --no-deps gork
```

### 防封版升级（只更新 gork，不动防封组件）

```bash
GORK_IMAGE=ghcr.io/dslzl/gork:<version-or-sha-tag>
docker pull "$GORK_IMAGE"
docker compose -f docker-compose.warp.yml up -d --no-deps gork
```

> `--no-deps` 参数确保只重启 gork 容器，WARP、Privoxy、FlareSolverr 不受影响，继续运行。

> `./data/` 目录中的配置文件（`config.toml`）和账号数据库（`accounts.db`）挂载在 volume 中，升级不会覆盖。

若使用 digest pinning，升级时先更新 `.env` 中的 `GORK_IMAGE` 或第三方镜像变量，再执行对应 compose up 命令。第三方镜像默认固定 digest，不会因为上游 `latest` 移动而自动变化。

### 回滚到指定版本

```bash
# 查看可用版本：https://github.com/dslzl/gork/pkgs/container/gork
docker pull ghcr.io/dslzl/gork:<tag>
docker compose up -d --no-deps gork
# 或防封版：
docker compose -f docker-compose.warp.yml up -d --no-deps gork
```

### 从标准版迁移到防封版

已有标准版部署的用户，迁移到防封版无需重新配置，数据完全保留：

```bash
# 1. 停止并删除当前 gork 容器（数据不受影响）
docker stop gork && docker rm gork

# 2. 进入项目目录（与标准版相同目录）
cd gork

# 3. 用防封版 compose 启动（会自动启动 WARP、Privoxy、FlareSolverr）
docker compose -f docker-compose.warp.yml up -d
```

> 防封版的 `init-config` 容器会检测 `data/config.toml` 是否已有代理配置：
> - 若已有配置（如之前手动配过代理）：跳过，不覆盖
> - 若无代理配置：自动写入 WARP + FlareSolverr 配置

迁移完成后，进入 Admin 后台确认代理配置已生效即可。

<br>

## 反向代理示例（Nginx）

```nginx
server {
    listen 443 ssl http2;
    server_name your.domain.com;

    location / {
        proxy_pass http://127.0.0.1:8000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 流式响应必备
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 600s;
        proxy_send_timeout 600s;
    }
}
```

完成反代后，记得在 Admin 后台把 `app.app_url` 改为 `https://your.domain.com`。

<br>

## WebUI

| 页面 | 路径 |
| :-- | :-- |
| Admin 登录页 | `/admin/login` |
| 账号管理 | `/admin/account` |
| 配置管理 | `/admin/config` |
| 缓存管理 | `/admin/cache` |
| WebUI 登录页 | `/webui/login` |
| Web Chat | `/webui/chat` |
| Masonry | `/webui/masonry` |
| ChatKit | `/webui/chatkit` |

### 鉴权规则

| 范围 | 配置项 | 规则 |
| :-- | :-- | :-- |
| `/v1/*` | `app.api_key` | 为空则不额外鉴权 |
| `/admin/*` | `app.app_key` | 为空时首次启动初始化为固定值 `gork` |
| `/webui/*` | `app.webui_enabled`, `app.webui_key` | 默认关闭；`webui_key` 为空则不额外校验 |

<br>

## 账号管理

### 账号类型

| 类型 | 说明 | 适用模型 |
| :-- | :-- | :-- |
| **付费账号** | x.ai 官方付费账号 | 所有 `grok-4.20-*`、`grok-4.3-beta`、`grok-4.3-fast` |
| **免费账号** | 通过 `console.x.ai` 访问的免费账号 | 所有 `*-console` 模型 |

### 免费账号配置

使用免费账号需要提供 SSO Token 与 CF Clearance：

1. 浏览器打开开发者工具（F12）
2. 访问 `https://console.x.ai/`
3. 在 Network 中找到任意请求，查看 Cookie：
   - 复制 `sso` 值
   - 复制 `cf_clearance` 值
4. 在 Admin 后台 → 账号管理 → 添加账号，将上述值填入对应字段

> SSO Token 与 CF Clearance 属于敏感凭证，请勿写入代码或提交到版本库。

### 账号池策略

| 策略/状态 | 说明 |
| :-- | :-- |
| quota 模式 | `account.refresh.enabled=true`，后台主动刷新配额，选号优先考虑 mode/tier、quota、冷却和并发。 |
| random 模式 | `account.refresh.enabled=false`，少做主动探测，依赖请求失败反馈和冷却窗口换号。 |
| `account.selection.max_inflight` | 单个账号允许同时承载的请求数；429 或超时较多时应降低。 |
| refresh interval | `basic_interval_sec`、`super_interval_sec`、`heavy_interval_sec` 控制不同账号池的刷新周期。 |
| invalid credentials | 连续达到 `account.invalid_credentials.max_failures` 后账号会被自动标记失效。 |
| rate limited | 429 会让账号进入冷却或降低选号优先级，具体行为取决于 quota/random 模式。 |
| SSO validation | `account.sso_validation.*` 用于定期验证 console.x.ai 免费账号的 SSO 可用性。 |

<br>

## 环境变量

启动期变量（`.env` / Compose / `docker run -e`）：

| 变量名 | 说明 | 默认值 |
| :-- | :-- | :-- |
| `TZ` | 时区 | `Asia/Shanghai` |
| `LOG_LEVEL` | 日志级别 | `INFO` |
| `LOG_FILE_ENABLED` | 写入本地文件日志 | `true` |
| `ACCOUNT_SYNC_INTERVAL` | 账号目录增量同步间隔（秒） | `30` |
| `ACCOUNT_SYNC_ACTIVE_INTERVAL` | 检测到变化后的活跃同步间隔（秒） | `3` |
| `SERVER_HOST` | 监听地址 | `0.0.0.0` |
| `SERVER_PORT` | 监听端口 | `8000` |
| `SERVER_WORKERS` | 旧 Python/Granian worker 变量；Go 运行时当前不读取，保留为镜像兼容占位 | `1` |
| `HOST_PORT` | Compose 宿主机映射端口 | `8000` |
| `DATA_DIR` | 本地数据根目录 | `./data` |
| `LOG_DIR` | 本地日志目录 | `./logs` |
| `ACCOUNT_STORAGE` | 账号存储后端：`local` / `redis` / `mysql` / `postgresql` | `local` |
| `ACCOUNT_LOCAL_PATH` | `local` 模式 SQLite 路径 | `${DATA_DIR}/accounts.db` |
| `ACCOUNT_REDIS_URL` | `redis` 账号存储 DSN；也可被 Redis runtime 复用 | `""` |
| `ACCOUNT_MYSQL_URL` | `mysql` 模式 DSN | `""` |
| `ACCOUNT_POSTGRESQL_URL` | `postgresql` 模式 DSN | `""` |
| `ACCOUNT_SQL_POOL_SIZE` | SQL 连接池核心连接数 | `5` |
| `ACCOUNT_SQL_MAX_OVERFLOW` | SQL 连接池最大溢出 | `10` |
| `ACCOUNT_SQL_POOL_TIMEOUT` | 等待空闲连接超时（秒） | `30` |
| `ACCOUNT_SQL_POOL_RECYCLE` | 连接最大复用时间（秒） | `1800` |
| `RUNTIME_REDIS_URL` | 可选 Redis runtime DSN，用于任务快照、调度选主等运行时协调；留空时回退本地行为 | `""` |
| `RUNTIME_TASK_TTL_S` | Redis task snapshot 保留时间（秒） | `300` |
| `RUNTIME_REDIS_LOCK_TTL_MS` | Redis scheduler leader 锁租约时间（毫秒） | `300000` |
| `CONFIG_LOCAL_PATH` | 运行时配置文件路径 | `${DATA_DIR}/config.toml` |

运行时配置也支持 `GROK_` 前缀环境变量覆盖。映射规则由配置 schema 生成：把配置 key 转成大写并把 `.` 替换为 `_`，例如 `GROK_APP_API_KEY` 覆盖 `app.api_key`，`GROK_FEATURES_STREAM` 覆盖 `features.stream`，`GROK_REVERSE_ENDPOINTS_BASE` 覆盖 `reverse.endpoints.base`。

配置上线前可先校验：

```bash
gork config validate --defaults config.defaults.toml --config ./data/config.toml
```

导出完整配置 schema 表：

```bash
gork config docs --defaults config.defaults.toml
```

完整配置表已生成到 [docs/configuration.md](docs/configuration.md)。README 只保留启动期变量和常用说明，避免手写表格与 `config.defaults.toml` 漂移。

### 可观测性与运维

- 每个 HTTP 请求都会注入 `X-Request-ID` 响应头；如果客户端传入同名 header，会沿用该值。access log 记录 method、脱敏 path、status、duration、request id，默认不写入 query。
- `[observability] metrics_enabled = true` 后开放 `/metrics`，输出 Prometheus 文本格式的 HTTP 请求数、请求耗时和上游错误状态码计数；默认关闭。
- `[observability] pprof_enabled = true` 后开放 `/debug/pprof/*`，用于临时 CPU/heap/goroutine 排查；默认关闭，建议只在内网或受保护环境启用。
- `/admin/api/status` 会返回 runtime、scheduler、proxy clearance、dynamic model refresh、media cache 和最近上游错误摘要。上游错误只保留状态码、分类消息和截断摘要，不保存完整敏感响应。
- media cache 状态包含 `limit_bytes`、`eviction_policy` 和最近 reconcile 报告。`cache.local.image_max_mb` / `video_max_mb` 为 `0` 时表示保存文件但不启用大小限制、索引、reconcile 或淘汰；大于 `0` 时启用 SQLite 索引和 LRU 淘汰，并回落到上限的 60%。
- Redis runtime 开启后，后台 batch task snapshot 会按 `RUNTIME_TASK_TTL_S` 保留，跨重启仍可查询最近任务进度；未配置 Redis 时仅使用进程内状态。
- `logging.max_files` 控制日志文件保留数量。日志按天写入 `app_{time:YYYY-MM-DD}.log`，到自然日切换时轮转；当前实现不按单文件大小切分，超过保留数量的旧日志由文件 sink 清理。

本地媒体缓存说明：

- URL 格式取决于 `features.image_format` / `features.video_format`：`grok_url` 直返上游 URL，`local_url` 返回本地代理 URL，`*_md` / `*_html` 返回可嵌入文本。
- 存储位置在 `DATA_DIR` 下的媒体缓存目录；容器部署时需要确保 `/app/data` 可写。
- `cache.local.image_max_mb` 和 `cache.local.video_max_mb` 为 `0` 时只保存文件，不启用大小索引和淘汰；大于 `0` 时启用 LRU 淘汰。
- 本地代理 URL 可能暴露生成内容，公网部署应配置 `app.app_url`、HTTPS 反代和签名 URL TTL。

### Redis 可选增强

Redis 不是必需依赖。默认 `ACCOUNT_STORAGE=local` 时项目会使用本地 SQLite 账号库和进程内运行时状态，适合单机/单 worker 部署。

需要以下能力时建议启用 Redis：

- 大量账号使用 Redis 存储，并通过二级索引优化 Admin 令牌列表分页、过滤和排序。
- 多 worker / 多副本部署时，用 Redis task snapshot 查询后台导入、批处理任务进度。
- 用 Redis leader lock 避免多个 worker 同时运行额度刷新调度器。

Docker Compose 可直接启用内置 Redis profile：

```bash
cp .env.example .env
# 编辑 .env：
# ACCOUNT_STORAGE=redis
# ACCOUNT_REDIS_URL=redis://redis:6379/0
# RUNTIME_REDIS_URL=redis://redis:6379/0
docker compose --profile redis up -d
```

若账号仍使用 SQLite/MySQL/PostgreSQL，但只想启用运行时协调，可保持 `ACCOUNT_STORAGE` 不变，仅设置 `RUNTIME_REDIS_URL`。

`ACCOUNT_REDIS_URL` 和 `RUNTIME_REDIS_URL` 可以指向同一个 Redis，但含义不同：前者保存账号、配额和索引，后者保存后台任务快照和调度锁。生产环境共用 Redis 时建议至少使用独立 DB 或清晰 key 前缀策略，并把 Redis 备份纳入恢复流程。

MySQL / PostgreSQL DSN 可直接使用 TLS/SSL 参数：

```env
ACCOUNT_MYSQL_URL=user:password@tcp(db.example.com:3306)/gork?parseTime=true&tls=true
ACCOUNT_POSTGRESQL_URL=postgres://user:password@db.example.com:5432/gork?sslmode=require
```

云数据库通常要求 `sslmode=require` / `verify-full` 或 MySQL TLS 配置。升级、回滚前应先备份 SQL 数据和 schema version 表；如果新版本已执行 schema 迁移，回滚镜像时应同步回滚到同一时间点的数据快照。

Compose 样例为 gork、Redis、WARP、Privoxy、FlareSolverr 和 demo reset 设置了基础 `mem_limit` / `cpus`，可通过 `GORK_MEM_LIMIT`、`REDIS_MEM_LIMIT`、`WARP_MEM_LIMIT` 等环境变量覆盖。资源限制只作为单机部署默认保护，生产环境应按账号数量、并发和代理负载调高。

### Demo reset 保护

`docker-compose.demo.yml` 的 `demo-reset` 容器默认不会清理数据库、Redis 或本地 volume。只有显式设置以下变量才会执行重置：

```bash
DEMO_RESET_CONFIRM=reset-demo-data
```

该保护用于避免 demo/reset 容器误连到真实 PostgreSQL 或 Redis 后执行清理。

更多 demo reset 行为见 [docs/demo-compose.md](docs/demo-compose.md)。

### 版本与更新检查

- 当前运行信息可通过 `/meta` 查看。
- 更新检查走 `/meta/update`，会请求 GitHub releases 并缓存结果；检查失败不会影响主 API。
- 如需关闭对外更新检查入口，可在反向代理层禁止访问 `/meta/update`，或仅允许内网/Admin 使用。
- 生产环境应固定 `GORK_IMAGE=ghcr.io/dslzl/gork:<version|sha-tag>` 或 digest，并在升级记录中保存当前 commit/image tag。

<br>

## 模型支持

> 通过 `GET /v1/models` 获取当前启用的模型列表。

### Chat（付费账号）

| 模型名 | mode | tier |
| :-- | :-- | :-- |
| `grok-4.20-0309-non-reasoning` | `fast` | `basic` |
| `grok-4.20-0309` | `auto` | `super` |
| `grok-4.20-0309-reasoning` | `expert` | `super` |
| `grok-4.20-0309-non-reasoning-super` | `fast` | `super` |
| `grok-4.20-0309-super` | `auto` | `super` |
| `grok-4.20-0309-reasoning-super` | `expert` | `super` |
| `grok-4.20-0309-non-reasoning-heavy` | `fast` | `heavy` |
| `grok-4.20-0309-heavy` | `auto` | `heavy` |
| `grok-4.20-0309-reasoning-heavy` | `expert` | `heavy` |
| `grok-4.20-multi-agent-0309` | `heavy` | `heavy` |
| `grok-4.20-fast` | `fast` | `basic`，优先使用高等级账号池 |
| `grok-4.20-auto` | `auto` | `super`，优先使用高等级账号池 |
| `grok-4.20-expert` | `expert` | `super`，优先使用高等级账号池 |
| `grok-4.20-heavy` | `heavy` | `heavy` |
| `grok-4.3-fast` | `fast` | `basic`，优先使用高等级账号池 |
| `grok-4.3-beta` | `grok-420-computer-use-sa` | `super` |

### Chat（console.x.ai 免费账号）

通过 `console.x.ai` 路由，使用 SSO Token 免费访问，不消耗付费账号额度。

| 模型名 | reasoning effort | 说明 |
| :-- | :-- | :-- |
| `grok-4.3-console` | 用户传入（默认 medium） | 免费账号 |
| `grok-4.3-low` | low（固定） | 免费账号 |
| `grok-4.3-medium` | medium（固定） | 免费账号 |
| `grok-4.3-high` | high（固定） | 免费账号 |
| `grok-4.20-0309-console` | 默认 | 免费账号 |
| `grok-4.20-0309-reasoning-console` | 固定 reasoning | 免费账号 |
| `grok-4.20-0309-non-reasoning-console` | 无 reasoning | 免费账号 |
| `grok-4.20-multi-agent-console` | 用户传入（默认 medium） | 免费账号，多智能体，agent 数量由 effort 决定 |
| `grok-4.20-multi-agent-low` | low（固定）→ 4 agents | 免费账号，多智能体 |
| `grok-4.20-multi-agent-medium` | medium（固定）→ 4 agents | 免费账号，多智能体 |
| `grok-4.20-multi-agent-high` | high（固定）→ 16 agents | 免费账号，多智能体 |
| `grok-4.20-multi-agent-xhigh` | xhigh（固定）→ 16 agents | 免费账号，多智能体 |
| `grok-build-console` | 默认 | 免费账号，Grok Build 0.1 |

> multi-agent 模型：`low`/`medium` 使用 4 个 agent（快速研究），`high`/`xhigh` 使用 16 个 agent（深度研究）。

### Console 模型配额

| 配额类型 | 次数 | 窗口 | 说明 |
| :-- | :-- | :-- | :-- |
| C（Console） | 30 次 | 15 分钟 | 所有 `*-console` / `*-low` / `*-medium` / `*-high` / `*-xhigh` 模型共享 |

<sub>以上数值基于简单压测得出（单账号约 40-50 次/5 分钟触发服务端限制），设为 30 次/15 分钟留有余量，避免触发上游真实 429。实际限制可能随 xAI 策略调整而变化。</sub>

Console 账号采用延迟恢复轮换策略：本地调用会扣减剩余额度，只有当剩余次数降到 15 次及以下时才启动 15 分钟恢复计时器；后台每 30 秒巡检并自动重置已过期的 Console 配额窗口。

### Image / Image Edit / Video

| 模型名 | mode | tier |
| :-- | :-- | :-- |
| `grok-imagine-image-lite` | `fast` | `basic` |
| `grok-imagine-image` | `auto` | `super` |
| `grok-imagine-image-pro` | `auto` | `super` |
| `grok-imagine-image-edit` | `auto` | `super` |
| `grok-imagine-video` | `auto` | `super` |

<br>

## API 一览

| 接口 | 鉴权 | 说明 |
| :-- | :-- | :-- |
| `GET /v1/models` | 是 | 列出当前启用模型 |
| `GET /v1/models/{model_id}` | 是 | 获取单个模型信息 |
| `POST /v1/chat/completions` | 是 | 对话 / 图像 / 视频统一入口 |
| `POST /v1/responses` | 是 | OpenAI Responses API 兼容子集 |
| `POST /v1/messages` | 是 | Anthropic Messages API 兼容接口 |
| `POST /v1/images/generations` | 是 | 独立图像生成接口 |
| `POST /v1/images/edits` | 是 | 独立图像编辑接口 |
| `POST /v1/videos` | 是 | 异步视频任务创建 |
| `GET /v1/videos/{video_id}` | 是 | 查询视频任务 |
| `GET /v1/videos/{video_id}/content` | 是 | 获取最终视频文件 |
| `GET /v1/files/video?id=...` | 否 | Gork 私有扩展：获取本地缓存视频，不属于 OpenAI 官方 Files API |
| `GET /v1/files/image?id=...` | 否 | Gork 私有扩展：获取本地缓存图片，不属于 OpenAI 官方 Files API |

<br>

## 调用示例

### 付费账号对话

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GORK_API_KEY" \
  -d '{
    "model": "grok-4.20-auto",
    "stream": true,
    "reasoning_effort": "high",
    "messages": [
      {"role":"user","content":"你好"}
    ]
  }'
```

### 免费账号对话

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GORK_API_KEY" \
  -d '{
    "model": "grok-4.3-high-console",
    "stream": true,
    "messages": [
      {"role":"user","content":"你好"}
    ]
  }'
```

### 图像生成

```bash
curl http://localhost:8000/v1/images/generations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GORK_API_KEY" \
  -d '{
    "model": "grok-imagine-image",
    "prompt": "一只在太空漂浮的猫",
    "n": 1,
    "size": "1792x1024",
    "response_format": "url"
  }'
```

### 视频生成

```bash
curl http://localhost:8000/v1/videos \
  -H "Authorization: Bearer $GORK_API_KEY" \
  -F "model=grok-imagine-video" \
  -F "prompt=霓虹雨夜街头，电影感慢镜头追拍" \
  -F "seconds=10" \
  -F "size=1792x1024" \
  -F "resolution_name=720p" \
  -F "preset=normal"
```

更完整的字段说明见上游 [接口文档](https://github.com/chenyme/grok2api#api-%E4%B8%80%E8%A7%88)。

<br>

## 贡献与分支规则

- `main` 是 Go 主线，普通功能、修复和文档更新都应基于 Go 主线提交。
- `python` 分支用于跟踪上游 Python mirror，不要把 Python mirror 文件混入 Go 主线提交。
- 需要移植上游 Python 变更时，先阅读并使用 `.codex/skills/port-python-upstream/SKILL.md`。
- PR 请附带对应验证命令；涉及配置、存储、代理、Admin/WebUI 或兼容 API 的改动应说明风险范围。

<br>

## 常见问题

**Q: 镜像启动后 `/admin/login` 打不开？**
确认容器端口映射正确：`docker compose ps` 查看 `0.0.0.0:8000->8000/tcp`，且宿主机防火墙未拦截。

**Q: 图片或视频链接返回 403？**
没有正确设置 `app.app_url`。该字段必须是用户能访问的公网地址（含协议），例如 `https://api.example.com`。

**Q: 提示 Cloudflare 拦截？**
在 Admin 后台 → 配置管理 → 代理配置，将 `proxy.clearance.mode` 设为 `manual` 并填入有效 `cf_cookies` + `user_agent`，或部署 FlareSolverr 后切到 `flaresolverr` 模式。

**Q: 当前版本是否已经修复 grok.com 403？**
A: 是。当前版本已内置 `x-statsig-id` 兼容修复，普通场景下无需额外浏览器 sidecar。若仍遇到 403，更多是出口 IP、Cloudflare 风控或 clearance 失效导致，建议优先尝试防封版部署。

**Q: 多 worker 部署？**
Go 版本当前是单进程 HTTP 服务，容器内不再通过 `SERVER_WORKERS` 启动多 worker。需要横向扩容时建议运行多个容器副本，并为账号存储、后台任务快照和运行时协调配置 Redis。

<br>

## 致谢

- 二开上游：[chenyme/grok2api](https://github.com/chenyme/grok2api)
- 三开上游：[jiujiu532/grok2api](https://github.com/jiujiu532/grok2api)
- DeepWiki：[chenyme/grok2api](https://deepwiki.com/chenyme/grok2api)
- 项目文档：[blog.cheny.me](https://blog.cheny.me/blog/posts/grok2api)
- 社区：[Linux.do](https://linux.do)

<br>

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=dslzl/gork&type=Date)](https://star-history.com/#dslzl/gork&Date)

<br>

## License

MIT
