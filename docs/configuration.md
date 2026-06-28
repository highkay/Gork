# Configuration Reference

This file is generated from `config.defaults.toml` through `gork config docs`. Do not hand-edit the table without updating the schema in `app/platform/config`.

Runtime config keys can be set in TOML or overridden by the listed `GROK_` environment names. Startup-only storage variables `ACCOUNT_STORAGE`, `ACCOUNT_REDIS_URL`, `ACCOUNT_MYSQL_URL`, `ACCOUNT_POSTGRESQL_URL`, `CONFIG_LOCAL_PATH`, `DATA_DIR`, and `RUNTIME_REDIS_URL` remain documented in README and operations docs because they are read before the hot-reload config snapshot is created.

| Key | Type | Default | Env | Hot reload | Sensitive | Description |
| :-- | :-- | :-- | :-- | :-- | :-- | :-- |
| `account.invalid_credentials.max_failures` | `int` | `3` | `GROK_ACCOUNT_INVALID_CREDENTIALS_MAX_FAILURES` | `true` | `false` | Consecutive invalid-credential failures before an account is expired. |
| `account.refresh.basic_interval_sec` | `int` | `86400` | `GROK_ACCOUNT_REFRESH_BASIC_INTERVAL_SEC` | `true` | `false` | Basic pool quota refresh interval in seconds. |
| `account.refresh.batch_timeout_sec` | `int` | `600` | `GROK_ACCOUNT_REFRESH_BATCH_TIMEOUT_SEC` | `true` | `false` | Total timeout in seconds for a quota refresh batch. |
| `account.refresh.enabled` | `bool` | `true` | `GROK_ACCOUNT_REFRESH_ENABLED` | `true` | `false` | Enables quota refresh mode; false uses random selection with retry feedback. |
| `account.refresh.heavy_interval_sec` | `int` | `7200` | `GROK_ACCOUNT_REFRESH_HEAVY_INTERVAL_SEC` | `true` | `false` | Heavy pool quota refresh interval in seconds. |
| `account.refresh.jitter_ratio` | `float` | `0.1` | `GROK_ACCOUNT_REFRESH_JITTER_RATIO` | `true` | `false` | Refresh scheduler jitter ratio applied to avoid synchronized refreshes. |
| `account.refresh.on_demand_min_interval_sec` | `int` | `300` | `GROK_ACCOUNT_REFRESH_ON_DEMAND_MIN_INTERVAL_SEC` | `true` | `false` | Minimum interval before an on-demand quota refresh can repeat. |
| `account.refresh.per_token_timeout_sec` | `int` | `30` | `GROK_ACCOUNT_REFRESH_PER_TOKEN_TIMEOUT_SEC` | `true` | `true` | Timeout in seconds for refreshing one token. |
| `account.refresh.run_on_start` | `bool` | `true` | `GROK_ACCOUNT_REFRESH_RUN_ON_START` | `true` | `false` | Runs quota refresh once at startup when refresh mode is enabled. |
| `account.refresh.super_interval_sec` | `int` | `7200` | `GROK_ACCOUNT_REFRESH_SUPER_INTERVAL_SEC` | `true` | `false` | Super pool quota refresh interval in seconds. |
| `account.refresh.usage_concurrency` | `int` | `50` | `GROK_ACCOUNT_REFRESH_USAGE_CONCURRENCY` | `true` | `false` | Concurrency for background usage refresh workers. |
| `account.selection.max_inflight` | `int` | `8` | `GROK_ACCOUNT_SELECTION_MAX_INFLIGHT` | `true` | `false` | Maximum concurrent requests leased to one account. |
| `account.sso_validation.batch_size` | `int` | `100` | `GROK_ACCOUNT_SSO_VALIDATION_BATCH_SIZE` | `true` | `false` | Number of SSO accounts validated per scheduled batch. |
| `account.sso_validation.concurrency` | `int` | `10` | `GROK_ACCOUNT_SSO_VALIDATION_CONCURRENCY` | `true` | `false` | Concurrency for scheduled SSO validation. |
| `account.sso_validation.enabled` | `bool` | `false` | `GROK_ACCOUNT_SSO_VALIDATION_ENABLED` | `true` | `false` | Enables scheduled validation for console.x.ai SSO accounts. |
| `account.sso_validation.interval_sec` | `int` | `300` | `GROK_ACCOUNT_SSO_VALIDATION_INTERVAL_SEC` | `true` | `false` | Scheduled SSO validation interval in seconds. |
| `account.sso_validation.max_failures` | `int` | `3` | `GROK_ACCOUNT_SSO_VALIDATION_MAX_FAILURES` | `true` | `false` | Consecutive SSO validation failures before an account is marked invalid. |
| `account.storage` | `string` | `local` | `GROK_ACCOUNT_STORAGE` | `false` | `false` | Account repository backend: local, redis, mysql, postgresql, or sqlite. |
| `app.admin_key` | `string` | `` | `GROK_APP_ADMIN_KEY` | `true` | `true` | Legacy Admin console password key; app.app_key is preferred. |
| `app.api_key` | `string` | `` | `GROK_APP_API_KEY` | `true` | `true` | API bearer token for /v1/* routes; empty disables API authentication. |
| `app.app_key` | `string` | `` | `GROK_APP_APP_KEY` | `true` | `true` | Admin console password; initialized to fixed value gork at first startup when empty. |
| `app.app_url` | `string` | `` | `GROK_APP_APP_URL` | `false` | `false` | Public base URL used to build local media links. |
| `app.name` | `string` | `grok` | `GROK_APP_NAME` | `true` | `false` | Application name used by config loaders and diagnostics. |
| `app.webui_enabled` | `bool` | `false` | `GROK_APP_WEBUI_ENABLED` | `true` | `false` | Enables the built-in WebUI pages. |
| `app.webui_key` | `string` | `` | `GROK_APP_WEBUI_KEY` | `true` | `true` | Optional WebUI password; empty allows WebUI access once enabled. |
| `asset.delete_timeout` | `int` | `60` | `GROK_ASSET_DELETE_TIMEOUT` | `true` | `false` | Timeout in seconds for upstream asset delete operations. |
| `asset.download_timeout` | `int` | `60` | `GROK_ASSET_DOWNLOAD_TIMEOUT` | `true` | `false` | Timeout in seconds for upstream asset download operations. |
| `asset.list_timeout` | `int` | `60` | `GROK_ASSET_LIST_TIMEOUT` | `true` | `false` | Timeout in seconds for upstream asset list operations. |
| `asset.upload_timeout` | `int` | `60` | `GROK_ASSET_UPLOAD_TIMEOUT` | `true` | `false` | Timeout in seconds for upstream asset upload operations. |
| `batch.asset_delete_concurrency` | `int` | `50` | `GROK_BATCH_ASSET_DELETE_CONCURRENCY` | `true` | `false` | Global asset delete concurrency, also used by admin batch cleanup defaults. |
| `batch.asset_list_concurrency` | `int` | `50` | `GROK_BATCH_ASSET_LIST_CONCURRENCY` | `true` | `false` | Global asset list concurrency shared by concurrent requests. |
| `batch.asset_upload_concurrency` | `int` | `10` | `GROK_BATCH_ASSET_UPLOAD_CONCURRENCY` | `true` | `false` | Global asset upload concurrency shared by attachment requests. |
| `batch.nsfw_concurrency` | `int` | `50` | `GROK_BATCH_NSFW_CONCURRENCY` | `true` | `false` | Per-token concurrency for admin NSFW enablement jobs. |
| `batch.refresh_concurrency` | `int` | `50` | `GROK_BATCH_REFRESH_CONCURRENCY` | `true` | `false` | Per-token concurrency for admin usage refresh jobs. |
| `cache.local.image_max_mb` | `int` | `0` | `GROK_CACHE_LOCAL_IMAGE_MAX_MB` | `true` | `false` | 0 stores images without indexing or eviction; values > 0 enable indexed LRU eviction. |
| `cache.local.video_max_mb` | `int` | `0` | `GROK_CACHE_LOCAL_VIDEO_MAX_MB` | `true` | `false` | 0 stores videos without indexing or eviction; values > 0 enable indexed LRU eviction. |
| `chat.timeout` | `int` | `60` | `GROK_CHAT_TIMEOUT` | `true` | `false` | Timeout in seconds for chat and responses requests. |
| `features.auto_chat_mode_fallback` | `bool` | `true` | `GROK_FEATURES_AUTO_CHAT_MODE_FALLBACK` | `true` | `false` | Falls back from auto quota to fast/expert chat modes when possible. |
| `features.custom_instruction` | `string` | `` | `GROK_FEATURES_CUSTOM_INSTRUCTION` | `true` | `false` | Global instruction appended to chat requests. |
| `features.dynamic_statsig` | `bool` | `true` | `GROK_FEATURES_DYNAMIC_STATSIG` | `true` | `false` | Generates dynamic Statsig identifiers for Grok web compatibility. |
| `features.enable_nsfw` | `bool` | `true` | `GROK_FEATURES_ENABLE_NSFW` | `true` | `false` | Allows NSFW image generation paths. |
| `features.image_format` | `string` | `grok_url` | `GROK_FEATURES_IMAGE_FORMAT` | `true` | `false` | Image response format: grok_url, local_url, markdown, HTML-compatible, or base64. |
| `features.imagine_public_image_proxy` | `bool` | `false` | `GROK_FEATURES_IMAGINE_PUBLIC_IMAGE_PROXY` | `true` | `false` | Downloads imagine-public images locally before returning URLs. |
| `features.memory` | `bool` | `false` | `GROK_FEATURES_MEMORY` | `true` | `false` | Enables conversation memory when supported by the upstream flow. |
| `features.show_search_sources` | `bool` | `false` | `GROK_FEATURES_SHOW_SEARCH_SOURCES` | `true` | `false` | Appends a plaintext Sources section in addition to structured search_sources. |
| `features.stream` | `bool` | `true` | `GROK_FEATURES_STREAM` | `true` | `false` | Enables streaming responses where the requested endpoint supports them. |
| `features.temporary` | `bool` | `true` | `GROK_FEATURES_TEMPORARY` | `true` | `false` | Uses temporary conversations where supported. |
| `features.thinking` | `bool` | `true` | `GROK_FEATURES_THINKING` | `true` | `false` | Includes thinking or reasoning output when available. |
| `features.thinking_summary` | `bool` | `false` | `GROK_FEATURES_THINKING_SUMMARY` | `true` | `false` | Returns a compact reasoning summary instead of full raw thinking text. |
| `features.video_format` | `string` | `grok_url` | `GROK_FEATURES_VIDEO_FORMAT` | `true` | `false` | Video response format: grok_url, local_url, grok_html, or local_html. |
| `image.stream_timeout` | `int` | `60` | `GROK_IMAGE_STREAM_TIMEOUT` | `true` | `false` | Timeout in seconds for streaming image generation. |
| `image.timeout` | `int` | `60` | `GROK_IMAGE_TIMEOUT` | `true` | `false` | Timeout in seconds for image generation and edit requests. |
| `logging.file_level` | `string` | `INFO` | `GROK_LOGGING_FILE_LEVEL` | `true` | `false` | Minimum level written to rotating local log files. |
| `logging.max_files` | `int` | `7` | `GROK_LOGGING_MAX_FILES` | `true` | `false` | Maximum number of daily log files retained. |
| `nsfw.timeout` | `int` | `60` | `GROK_NSFW_TIMEOUT` | `true` | `false` | Timeout in seconds for NSFW enablement requests. |
| `observability.metrics_enabled` | `bool` | `false` | `GROK_OBSERVABILITY_METRICS_ENABLED` | `true` | `false` | Exposes Prometheus metrics at /metrics. |
| `observability.pprof_enabled` | `bool` | `false` | `GROK_OBSERVABILITY_PPROF_ENABLED` | `true` | `false` | Exposes Go pprof endpoints under /debug/pprof. |
| `proxy.clearance.browser` | `string` | `chrome136` | `GROK_PROXY_CLEARANCE_BROWSER` | `true` | `false` | curl_cffi browser fingerprint used for manual Cloudflare clearance. |
| `proxy.clearance.cf_cookies` | `string` | `` | `GROK_PROXY_CLEARANCE_CF_COOKIES` | `true` | `true` | Manual Cloudflare Cookie header value. |
| `proxy.clearance.flaresolverr_url` | `string` | `http://flaresolverr:8191` | `GROK_PROXY_CLEARANCE_FLARESOLVERR_URL` | `false` | `false` | FlareSolverr service URL used to refresh Cloudflare clearance. |
| `proxy.clearance.mode` | `string` | `flaresolverr` | `GROK_PROXY_CLEARANCE_MODE` | `true` | `false` | Cloudflare clearance mode: none, manual, or flaresolverr. |
| `proxy.clearance.refresh_interval` | `int` | `3600` | `GROK_PROXY_CLEARANCE_REFRESH_INTERVAL` | `true` | `false` | Cloudflare clearance refresh interval in seconds. |
| `proxy.clearance.timeout_sec` | `int` | `60` | `GROK_PROXY_CLEARANCE_TIMEOUT_SEC` | `true` | `false` | Cloudflare challenge wait timeout in seconds. |
| `proxy.clearance.user_agent` | `string` | `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36` | `GROK_PROXY_CLEARANCE_USER_AGENT` | `true` | `false` | User-Agent that must match the clearance cookie source browser. |
| `proxy.egress.mode` | `string` | `single_proxy` | `GROK_PROXY_EGRESS_MODE` | `true` | `false` | Outbound proxy mode: direct, single_proxy, or proxy_pool. |
| `proxy.egress.proxy_pool` | `string_list` | `[]` | `GROK_PROXY_EGRESS_PROXY_POOL` | `true` | `false` | Proxy pool for API traffic when proxy_pool mode is enabled. |
| `proxy.egress.proxy_url` | `string` | `http://privoxy:8118` | `GROK_PROXY_EGRESS_PROXY_URL` | `false` | `false` | Single proxy URL for API traffic. |
| `proxy.egress.resource_proxy_pool` | `string_list` | `[]` | `GROK_PROXY_EGRESS_RESOURCE_PROXY_POOL` | `true` | `false` | Proxy pool for image/video downloads; falls back to proxy_pool. |
| `proxy.egress.resource_proxy_url` | `string` | `http://privoxy:8118` | `GROK_PROXY_EGRESS_RESOURCE_PROXY_URL` | `false` | `false` | Proxy URL for image/video downloads; falls back to proxy_url. |
| `proxy.egress.skip_ssl_verify` | `bool` | `false` | `GROK_PROXY_EGRESS_SKIP_SSL_VERIFY` | `true` | `false` | Skips proxy TLS certificate validation for self-signed proxy endpoints. |
| `retry.max_retries` | `int` | `1` | `GROK_RETRY_MAX_RETRIES` | `true` | `false` | Maximum application-level account-switch retries; 0 disables retries. |
| `retry.on_codes` | `string` | `429,401,503` | `GROK_RETRY_ON_CODES` | `true` | `false` | Comma-separated HTTP status codes that trigger account-switch retries. |
| `retry.reset_session_status_codes` | `string_list` | `[403]` | `GROK_RETRY_RESET_SESSION_STATUS_CODES` | `true` | `false` | HTTP status codes that rebuild transport proxy sessions. |
| `retry.retry_status_codes` | `string` | `429,401,503` | `GROK_RETRY_RETRY_STATUS_CODES` | `true` | `false` | Legacy retry HTTP status code key used by runtime compatibility paths. |
| `reverse.endpoints.accounts_base` | `string` | `https://accounts.x.ai` | `GROK_REVERSE_ENDPOINTS_ACCOUNTS_BASE` | `true` | `false` | Base URL for x.ai account and SSO endpoints. |
| `reverse.endpoints.assets_cdn` | `string` | `https://assets.grok.com` | `GROK_REVERSE_ENDPOINTS_ASSETS_CDN` | `true` | `false` | Base URL for Grok asset CDN requests. |
| `reverse.endpoints.base` | `string` | `https://grok.com` | `GROK_REVERSE_ENDPOINTS_BASE` | `true` | `false` | Base URL for Grok web API requests. |
| `reverse.endpoints.console_base` | `string` | `https://console.x.ai` | `GROK_REVERSE_ENDPOINTS_CONSOLE_BASE` | `true` | `false` | Base URL for console.x.ai free-account flows. |
| `reverse.endpoints.console_cluster` | `string` | `https://us-east-1.api.x.ai` | `GROK_REVERSE_ENDPOINTS_CONSOLE_CLUSTER` | `true` | `false` | Console API cluster URL used by free-account model calls. |
| `reverse.endpoints.ws_livekit` | `string` | `wss://livekit.grok.com` | `GROK_REVERSE_ENDPOINTS_WS_LIVEKIT` | `true` | `false` | LiveKit WebSocket URL used by realtime and voice flows. |
| `security.cors.api_allowed_origins` | `string_list` | `[]` | `GROK_SECURITY_CORS_API_ALLOWED_ORIGINS` | `true` | `false` | Additional allowed origins for API CORS requests. |
| `security.cors.web_allowed_origins` | `string_list` | `[]` | `GROK_SECURITY_CORS_WEB_ALLOWED_ORIGINS` | `true` | `false` | Additional allowed origins for WebUI/Admin CORS and WebSocket requests. |
| `security.headers.hsts_enabled` | `bool` | `false` | `GROK_SECURITY_HEADERS_HSTS_ENABLED` | `true` | `false` | Enables Strict-Transport-Security response headers. |
| `security.media.signed_url_ttl_seconds` | `int` | `3600` | `GROK_SECURITY_MEDIA_SIGNED_URL_TTL_SECONDS` | `true` | `false` | Signed local media URL lifetime in seconds. |
| `security.websocket.max_connections` | `int` | `128` | `GROK_SECURITY_WEBSOCKET_MAX_CONNECTIONS` | `true` | `false` | Maximum concurrent WebUI WebSocket connections. |
| `security.websocket.max_connections_per_ip` | `int` | `16` | `GROK_SECURITY_WEBSOCKET_MAX_CONNECTIONS_PER_IP` | `true` | `false` | Maximum concurrent WebUI WebSocket connections per client IP. |
| `security.websocket.max_message_bytes` | `int` | `1048576` | `GROK_SECURITY_WEBSOCKET_MAX_MESSAGE_BYTES` | `true` | `false` | Maximum WebUI WebSocket message size in bytes. |
| `security.websocket.read_timeout_seconds` | `int` | `60` | `GROK_SECURITY_WEBSOCKET_READ_TIMEOUT_SECONDS` | `true` | `false` | WebUI WebSocket read timeout in seconds. |
| `security.websocket.write_timeout_seconds` | `int` | `15` | `GROK_SECURITY_WEBSOCKET_WRITE_TIMEOUT_SECONDS` | `true` | `false` | WebUI WebSocket write timeout in seconds. |
| `server.max_header_bytes` | `int` | `0` | `GROK_SERVER_MAX_HEADER_BYTES` | `true` | `false` | HTTP server maximum request header size in bytes; 0 uses Go defaults. |
| `startup.migration.account_batch_size` | `int` | `500` | `GROK_STARTUP_MIGRATION_ACCOUNT_BATCH_SIZE` | `true` | `false` | Batch size for startup account storage migrations. |
| `video.timeout` | `int` | `60` | `GROK_VIDEO_TIMEOUT` | `true` | `false` | Timeout in seconds for video generation and polling. |
| `voice.timeout` | `int` | `60` | `GROK_VOICE_TIMEOUT` | `true` | `false` | Timeout in seconds for voice and realtime requests. |
