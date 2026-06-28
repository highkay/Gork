<img alt="Gork" src="https://github.com/user-attachments/assets/037a0a6e-7986-41cc-b4af-04df612ee886" />

[![Go](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![OpenAI Compatible](https://img.shields.io/badge/API-OpenAI%20compatible-111827)](#api-endpoints)
[![Version](https://img.shields.io/badge/version-2.0.4.rc4-111827)](../go.mod)
[![License](https://img.shields.io/badge/license-MIT-16a34a)](../LICENSE)
[![Docker](https://img.shields.io/badge/ghcr.io-dslzl%2Fgork-2496ED?logo=docker&logoColor=white)](https://github.com/dslzl/gork/pkgs/container/gork)
[![中文](https://img.shields.io/badge/%E4%B8%AD%E6%96%87-DC2626?logo=bookstack&logoColor=white)](../README.md)

> [!NOTE]
> This project is for learning and research only. You must comply with Grok's Terms of Service and your local laws. Do not use it for unlawful purposes. Forks and PRs should preserve original author and frontend attribution.

<br>

Gork is a **Go**-based Grok gateway that exposes Grok's web capabilities through OpenAI-compatible APIs. Highlights:

- OpenAI-compatible endpoints: `/v1/models`, `/v1/chat/completions`, `/v1/responses`, `/v1/images/generations`, `/v1/images/edits`, `/v1/videos`, `/v1/videos/{video_id}`, `/v1/videos/{video_id}/content`
- Anthropic-compatible endpoint: `/v1/messages`
- Streaming and non-streaming chat, explicit reasoning output, function tools passthrough, unified token / usage accounting
- Multi-account pool, tiered selection, failure feedback, quota sync and auto maintenance
- Local image / video caching with reverse-proxied URLs
- Text-to-image, image edit, text-to-video, image-to-video
- Built-in Admin console, Web Chat, Masonry image gallery, ChatKit voice page
- `console.x.ai` free account support with a dedicated `*-console` model family
- Optional Redis runtime coordination, Redis/SQL account storage, WARP/Privoxy/FlareSolverr anti-blocking stack

<br>

## Documentation

| Document | Contents |
| :-- | :-- |
| [Architecture](architecture.md) | Module boundaries, request flow, and dependency rules |
| [Configuration](configuration.md) | Generated TOML / `GROK_` environment variable reference |
| [API Compatibility](api-compatibility.md) | OpenAI, Anthropic, and private Admin/WebUI compatibility notes |
| [Security](security.md) | Secrets, auth, CORS, media URLs, redaction, TLS, and container hardening |
| [Operations](operations.md) | Docker, Compose, anti-blocking stack, Redis, SQL, logs, upgrade, backup, restore |
| [Troubleshooting](troubleshooting.md) | 401, 403, 429, 5xx, Cloudflare, LiveKit/WebSocket, asset upload |
| [Demo Compose](demo-compose.md) | Demo-only compose stack and reset behavior |
| [中文 README](../README.md) | Chinese quick start |

<br>

## Image Info

This repository is a fork based on [chenyme/grok2api](https://github.com/chenyme/grok2api) and [jiujiu532/grok2api](https://github.com/jiujiu532/grok2api), and ships a prebuilt Docker image:

| Field | Value |
| :-- | :-- |
| Default image | `ghcr.io/dslzl/gork:latest` (use a version, sha, or digest in production) |
| Architecture | `linux/amd64`, `linux/arm64` |
| Base image | Go static binary runtime image |
| Default port | `8000` |
| Data dir | `/app/data` |
| Logs dir | `/app/logs` |

<br>

## Quick Start

Choose the simplest stack that works:

1. Start with the standard compose stack and run `/health`, `/v1/models`, and one minimal chat smoke test.
2. Stay on the standard stack if it works; it has fewer moving parts and lower upgrade risk.
3. Switch to the WARP stack only when you see recurring 403, Cloudflare challenge, or unstable clearance/proxy behavior.
4. If you only need Redis, use the standard compose Redis profile instead of the WARP stack.

### Option 1: Docker Compose (recommended)

```bash
git clone https://github.com/dslzl/gork
cd gork
cp .env.example .env
docker compose up -d
```

Tail logs:

```bash
docker compose logs -f gork
```

> The included `docker-compose.yml` uses `GORK_IMAGE`, defaulting to `ghcr.io/dslzl/gork:latest`. For production, set `GORK_IMAGE=ghcr.io/dslzl/gork:<version-or-sha-tag>` or pin a digest.

### Option 2: WARP + FlareSolverr stack

Use this only when the standard stack is blocked by Cloudflare or proxy quality problems.

```bash
git clone https://github.com/dslzl/gork
cd gork
docker compose -f docker-compose.warp.yml up -d
```

The stack starts:

| Service | Purpose |
| :-- | :-- |
| `warp-proxy` | Cloudflare WARP egress proxy |
| `privoxy` | HTTP proxy forwarding to WARP |
| `flaresolverr` | Cloudflare challenge solver |
| `gork` | Main service |

### Option 3: Plain Docker

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

Windows PowerShell:

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

### Option 4: From source

Prerequisites: Go 1.25+. Python 3.13+ and `uv` are only needed for migration-period regression tests.

```bash
git clone https://github.com/dslzl/gork
cd gork
cp .env.example .env
go run ./cmd/gork

# Optional: build a local binary
go build -o gork ./cmd/gork
./gork
```

### First-time setup

On first startup, the service writes the fixed initial Admin key `gork` to `app.app_key` and prints it to the logs. Open `http://localhost:8000/admin/login`, sign in with that initial key, then:

1. Change `app.app_key` (Admin console password)
2. Set `app.api_key` (API auth key; leave empty to disable auth)
3. Set `app.app_url` (publicly reachable base URL; otherwise image / video links return 403)

> `config.defaults.toml` is the only default source; `config.example.toml` is a sample. Runtime config is persisted to `${DATA_DIR}/config.toml` and applied immediately. No container restart is required. Admin saves normalize TOML and do not preserve handwritten comments.

<br>

## Upgrade and Rollback

```bash
# Upgrade to a specific published tag
GORK_IMAGE=ghcr.io/dslzl/gork:<version-or-sha-tag>
docker pull "$GORK_IMAGE"
docker compose up -d

# Rollback
docker run -d ... ghcr.io/dslzl/gork:<tag>
```

<br>

## Reverse Proxy (Nginx example)

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

        # Required for streaming
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 600s;
        proxy_send_timeout 600s;
    }
}
```

After enabling the reverse proxy, set `app.app_url` to `https://your.domain.com` in the Admin console.

<br>

## WebUI

| Page | Path |
| :-- | :-- |
| Admin login | `/admin/login` |
| Account management | `/admin/account` |
| Config management | `/admin/config` |
| Cache management | `/admin/cache` |
| WebUI login | `/webui/login` |
| Web Chat | `/webui/chat` |
| Masonry | `/webui/masonry` |
| ChatKit | `/webui/chatkit` |

### Authentication

| Scope | Config | Rule |
| :-- | :-- | :-- |
| `/v1/*` | `app.api_key` | No auth when empty |
| `/admin/*` | `app.app_key` | Initializes to the fixed value `gork` on first startup when empty |
| `/webui/*` | `app.webui_enabled`, `app.webui_key` | Disabled by default; no extra check when `webui_key` is empty |

<br>

## Account Management

### Account types

| Type | Description | Models |
| :-- | :-- | :-- |
| **Paid account** | Official x.ai paid account | All `grok-4.20-*`, `grok-4.3-beta` |
| **Free account** | Free account via `console.x.ai` | All `*-console` models |

### Free account setup

To use free accounts you need both an SSO Token and a CF Clearance:

1. Open browser DevTools (F12)
2. Visit `https://console.x.ai/`
3. In the Network tab, inspect any request's cookies and copy:
   - the `sso` value
   - the `cf_clearance` value
4. In Admin → Account → Add account, paste both values into the matching fields

> SSO Token and CF Clearance are sensitive credentials. Never commit them to source control.

### Account pool strategy

| Strategy / state | Meaning |
| :-- | :-- |
| Quota mode | `account.refresh.enabled=true`; background jobs refresh quota and selection uses mode/tier, quota, cooldown, and inflight limits. |
| Random mode | `account.refresh.enabled=false`; less probing, with account switching driven by request failures and cooldowns. |
| `account.selection.max_inflight` | Maximum concurrent requests leased to one account. Lower it when 429 or timeout frequency rises. |
| Refresh intervals | `basic_interval_sec`, `super_interval_sec`, and `heavy_interval_sec` control refresh cadence per pool. |
| Invalid credentials | Accounts are expired after `account.invalid_credentials.max_failures` consecutive invalid-credential failures. |
| Rate limited | 429 responses push accounts into cooldown or lower selection priority, depending on quota/random mode. |
| SSO validation | `account.sso_validation.*` schedules validation for console.x.ai free-account SSO credentials. |

<br>

## Environment Variables

Bootstrap-time variables (`.env` / Compose / `docker run -e`):

| Name | Description | Default |
| :-- | :-- | :-- |
| `TZ` | Timezone | `Asia/Shanghai` |
| `LOG_LEVEL` | Log level | `INFO` |
| `LOG_FILE_ENABLED` | Write file logs | `true` |
| `ACCOUNT_SYNC_INTERVAL` | Account directory sync interval (s) | `30` |
| `ACCOUNT_SYNC_ACTIVE_INTERVAL` | Active sync interval after a change (s) | `3` |
| `SERVER_HOST` | Listen host | `0.0.0.0` |
| `SERVER_PORT` | Listen port | `8000` |
| `SERVER_WORKERS` | Legacy Python/Granian worker variable; the Go runtime does not read it, kept as an image compatibility placeholder | `1` |
| `HOST_PORT` | Compose host port mapping | `8000` |
| `DATA_DIR` | Data root | `./data` |
| `LOG_DIR` | Logs dir | `./logs` |
| `ACCOUNT_STORAGE` | Backend: `local` / `redis` / `mysql` / `postgresql` | `local` |
| `ACCOUNT_LOCAL_PATH` | SQLite path for `local` mode | `${DATA_DIR}/accounts.db` |
| `ACCOUNT_REDIS_URL` | DSN for `redis` mode | `""` |
| `ACCOUNT_MYSQL_URL` | DSN for `mysql` mode | `""` |
| `ACCOUNT_POSTGRESQL_URL` | DSN for `postgresql` mode | `""` |
| `ACCOUNT_SQL_POOL_SIZE` | SQL pool core size | `5` |
| `ACCOUNT_SQL_MAX_OVERFLOW` | SQL pool max overflow | `10` |
| `ACCOUNT_SQL_POOL_TIMEOUT` | Pool checkout timeout (s) | `30` |
| `ACCOUNT_SQL_POOL_RECYCLE` | Connection recycle time (s) | `1800` |
| `RUNTIME_REDIS_URL` | Optional Redis DSN for task snapshots and scheduler lock | `""` |
| `RUNTIME_TASK_TTL_S` | Redis task snapshot retention (s) | `300` |
| `RUNTIME_REDIS_LOCK_TTL_MS` | Redis scheduler leader lock TTL (ms) | `300000` |
| `CONFIG_LOCAL_PATH` | Runtime config file path | `${DATA_DIR}/config.toml` |

Runtime config can also be overridden via `GROK_`-prefixed env vars. The schema maps a config key by uppercasing it and replacing `.` with `_`, e.g. `GROK_APP_API_KEY` overrides `app.api_key`, `GROK_FEATURES_STREAM` overrides `features.stream`, and `GROK_REVERSE_ENDPOINTS_BASE` overrides `reverse.endpoints.base`.

Validate config before rollout:

```bash
gork config validate --defaults config.defaults.toml --config ./data/config.toml
```

Export the full config schema table:

```bash
gork config docs --defaults config.defaults.toml
```

The generated full reference is committed at [configuration.md](configuration.md). Keep startup-only variables in this README and runtime TOML keys in the generated reference.

Redis account storage and Redis runtime are separate:

| Feature | Variable | Purpose |
| :-- | :-- | :-- |
| Account storage | `ACCOUNT_STORAGE=redis` + `ACCOUNT_REDIS_URL` | Stores accounts, quotas, status, and Admin list indexes. |
| Runtime coordination | `RUNTIME_REDIS_URL` | Stores task snapshots and scheduler leader locks; does not store accounts. |

SQL DSNs may include cloud-database TLS/SSL parameters:

```env
ACCOUNT_MYSQL_URL=user:password@tcp(db.example.com:3306)/gork?parseTime=true&tls=true
ACCOUNT_POSTGRESQL_URL=postgres://user:password@db.example.com:5432/gork?sslmode=require
```

<br>

## Models

> Use `GET /v1/models` to fetch the live list.

### Chat (paid)

| Model | mode | tier |
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
| `grok-4.20-fast` | `fast` | `basic`, prefers higher-tier accounts |
| `grok-4.20-auto` | `auto` | `super`, prefers higher-tier accounts |
| `grok-4.20-expert` | `expert` | `super`, prefers higher-tier accounts |
| `grok-4.20-heavy` | `heavy` | `heavy` |
| `grok-4.3-beta` | `grok-420-computer-use-sa` | `super` |

### Chat (console.x.ai free)

| Model | reasoning effort | Notes |
| :-- | :-- | :-- |
| `grok-4-console` | default | Free account |
| `grok-4.3-console` | medium | Free account |
| `grok-4.3-low-console` | low | Free account |
| `grok-4.3-medium-console` | medium | Free account |
| `grok-4.3-high-console` | high | Free account |
| `grok-4.20-0309-console` | default | Free account |
| `grok-4.20-0309-reasoning-console` | fixed reasoning | Free account |
| `grok-4.20-multi-agent-console` | default | Free account, multi-agent |

### Image / Image Edit / Video

| Model | mode | tier |
| :-- | :-- | :-- |
| `grok-imagine-image-lite` | `fast` | `basic` |
| `grok-imagine-image` | `auto` | `super` |
| `grok-imagine-image-pro` | `auto` | `super` |
| `grok-imagine-image-edit` | `auto` | `super` |
| `grok-imagine-video` | `auto` | `super` |

<br>

## API Reference

| Endpoint | Auth | Description |
| :-- | :-- | :-- |
| `GET /v1/models` | yes | List enabled models |
| `GET /v1/models/{model_id}` | yes | Get a single model |
| `POST /v1/chat/completions` | yes | Unified chat / image / video entry |
| `POST /v1/responses` | yes | OpenAI Responses API subset |
| `POST /v1/messages` | yes | Anthropic Messages API |
| `POST /v1/images/generations` | yes | Standalone image generation |
| `POST /v1/images/edits` | yes | Standalone image editing |
| `POST /v1/videos` | yes | Async video job creation |
| `GET /v1/videos/{video_id}` | yes | Query a video job |
| `GET /v1/videos/{video_id}/content` | yes | Download the final video |
| `GET /v1/files/video?id=...` | no | Gork private extension for locally cached video; not part of the official OpenAI Files API |
| `GET /v1/files/image?id=...` | no | Gork private extension for locally cached image; not part of the official OpenAI Files API |

<br>

## Examples

### Paid account chat

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GORK_API_KEY" \
  -d '{
    "model": "grok-4.20-auto",
    "stream": true,
    "reasoning_effort": "high",
    "messages": [
      {"role":"user","content":"Hello"}
    ]
  }'
```

### Free account chat

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GORK_API_KEY" \
  -d '{
    "model": "grok-4.3-high-console",
    "stream": true,
    "messages": [
      {"role":"user","content":"Hello"}
    ]
  }'
```

### Image generation

```bash
curl http://localhost:8000/v1/images/generations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GORK_API_KEY" \
  -d '{
    "model": "grok-imagine-image",
    "prompt": "A cat floating in outer space",
    "n": 1,
    "size": "1792x1024",
    "response_format": "url"
  }'
```

### Video generation

```bash
curl http://localhost:8000/v1/videos \
  -H "Authorization: Bearer $GORK_API_KEY" \
  -F "model=grok-imagine-video" \
  -F "prompt=Neon rainy night street, cinematic slow-motion tracking shot" \
  -F "seconds=10" \
  -F "size=1792x1024" \
  -F "resolution_name=720p" \
  -F "preset=normal"
```

For full field references see the upstream [API docs](https://github.com/chenyme/grok2api#api-%E4%B8%80%E8%A7%88).

<br>

## Version, Updates, and Contributions

- `/meta` exposes current runtime metadata.
- `/meta/update` checks GitHub releases and caches the result; failures do not affect the main API.
- To disable public update checks, block `/meta/update` at the reverse proxy or expose it only internally.
- Production deployments should pin `GORK_IMAGE` to a version tag, `sha-<commit>` tag, or digest.
- `main` is the Go branch; `python` mirrors upstream Python work. Use `.codex/skills/port-python-upstream/SKILL.md` before porting Python upstream changes.
- PRs should include verification commands and call out risk areas for config, storage, proxy, Admin/WebUI, or compatibility API changes.

<br>

## FAQ

**Q: `/admin/login` is unreachable after the container starts.**
Check the port mapping with `docker compose ps` (expect `0.0.0.0:8000->8000/tcp`) and verify your host firewall allows it.

**Q: Image / video URLs return 403.**
`app.app_url` is missing or wrong. It must be a fully qualified URL that clients can reach (e.g. `https://api.example.com`).

**Q: Cloudflare keeps blocking requests.**
In Admin → Config → Proxy, switch `proxy.clearance.mode` to `manual` and provide matching `cf_cookies` + `user_agent`, or deploy FlareSolverr and switch to the `flaresolverr` mode.

**Q: Multi-worker deployment.**
The Go version currently runs as a single-process HTTP service and no longer starts in-container workers through `SERVER_WORKERS`. For horizontal scaling, run multiple container replicas and configure Redis for account storage, task snapshots, and runtime coordination.

**Q: Observability and operations.**
Every request receives an `X-Request-ID` response header, and access logs record method, sanitized path, status, duration, and request id without raw query strings. Enable `[observability] metrics_enabled = true` to expose Prometheus text metrics at `/metrics`; enable `[observability] pprof_enabled = true` to expose `/debug/pprof/*`. Both are disabled by default. `/admin/api/status` includes runtime, scheduler, proxy clearance, dynamic model refresh, media cache, and recent upstream error summaries. Media cache status includes `limit_bytes`, `eviction_policy`, and the latest reconcile report; `cache.local.image_max_mb` / `video_max_mb` set to `0` means files are saved without size limiting, indexing, reconcile, or eviction. Redis runtime task snapshots are retained according to `RUNTIME_TASK_TTL_S`, so recent batch task progress can be queried across restarts. `logging.max_files` is daily-file retention for `app_{time:YYYY-MM-DD}.log`; logs rotate on date change, not by file size.

<br>

## Credits

- Upstream: [chenyme/grok2api](https://github.com/chenyme/grok2api)
- DeepWiki: [chenyme/grok2api](https://deepwiki.com/chenyme/grok2api)
- Project blog: [blog.cheny.me](https://blog.cheny.me/blog/posts/grok2api)

<br>

## License

MIT
