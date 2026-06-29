# Troubleshooting

排障时先记录 request id、HTTP status、时间、使用模型、账号池状态和部署方式。不要在日志或 issue 中贴完整 token、cookie、SSO 或 Authorization header。

## 401 Unauthorized

可能原因：

- `/v1/*` 请求缺少 `Authorization: Bearer <app.api_key>`。
- Admin/WebUI 密码错误。
- 上游返回 invalid credentials，账号 token 已失效。

处理：

1. 确认 `app.api_key`、`app.app_key`、`app.webui_key` 的实际配置。
2. 在 Admin 账号页查看账号状态和最近失败原因。
3. 对免费账号重新采集 SSO token 和 `cf_clearance`。

## 403 Forbidden

可能原因：

- 出口 IP 被 Cloudflare 风控。
- `cf_clearance` 与 User-Agent 不匹配或过期。
- 上游 Grok Web 路由变化。
- 本地媒体 URL 缺少有效签名或 TTL 过期。

处理：

1. 标准版先确认服务器能直连 `https://grok.com`。
2. 仍 403 时切换防封版或配置可用代理。
3. 使用 manual clearance 时，确保 Cookie 与 User-Agent 来自同一次浏览器会话。
4. 媒体 URL 403 时确认 `app.app_url`、签名 TTL 和反代路径。

## 429 Rate Limited

可能原因：

- 单账号额度耗尽。
- `account.selection.max_inflight` 太高。
- 账号池过小或所有账号进入冷却。

处理：

1. 查看 Admin 账号页的 quota、status、cooldown 和 fail reason。
2. 降低 `account.selection.max_inflight`。
3. 开启或调整 `account.refresh.*`。
4. 增加账号池，或将高并发请求分散到不同 pool。

## 5xx

可能原因：

- 上游 Grok 临时失败。
- 代理或 Byparr 不可用。
- 本地数据库、Redis 或文件系统异常。

处理：

1. 查看 gork 日志里的 request id 和 upstream status。
2. 检查 `/health`、`/meta`、`/admin/api/status`。
3. Redis/SQL 模式下检查连接 DSN、TLS 参数和网络访问。
4. 临时切回 local storage 或直连代理做对比验证。

## Cloudflare Challenge

表现：

- 403、challenge 页面、clearance 刷新失败、Byparr 超时。

处理：

1. 标准版优先确认是否真的需要防封版。
2. 防封版检查 `warp-proxy`、`privoxy`、`byparr` 三个容器是否已启动。
3. 调高 `proxy.clearance.timeout_sec`。
4. 手动模式下重新采集 `cf_clearance` 与 User-Agent。

## LiveKit and WebSocket

可能原因：

- 反向代理未保留 WebSocket upgrade。
- `reverse.endpoints.ws_livekit` 被改错。
- 代理不支持 WebSocket 长连接。

处理：

1. Nginx/Caddy 反代启用 WebSocket upgrade。
2. 关闭 proxy buffering，并提高 read timeout。
3. 如果只 WebSocket 失败，先绕过代理或更换支持 WS 的代理。

## Asset Upload

可能原因：

- 附件过大或格式不被上游接受。
- `batch.asset_upload_concurrency` 太高。
- 临时目录或 `/app/data` 不可写。
- 上游 asset list/delete 超时。

处理：

1. 确认容器内 `/app/data` 可写。
2. 降低 `batch.asset_upload_concurrency`。
3. 调整 `asset.upload_timeout`、`asset.list_timeout`、`asset.delete_timeout`。
4. 在 Admin 缓存页清理异常缓存项。
