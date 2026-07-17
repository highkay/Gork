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

## SSO 校验大量 Cloudflare / 未删无效号

表现：

- Admin 或 `scripts/sso_sweep_progress.py` 里 `last_fail_stage=cloudflare` 暴涨。
- `sso-sweep` 的 `cf` 计数很高，但 `session_invalid` / `deleted` 很少。
- 定时 `account.sso_validation` 几乎不 expire，号池里仍有大量 dead SSO。

可能原因：

- 探针 clearance 绑到了 `grok.com` 而不是 `accounts.x.ai`（cookie 按 host 隔离）。
- 出口 IP / 代理被 Cloudflare 挑战，accounts 导航无法完成。
- 把 soft-fail（CF/WAF/限流）误当成 session 死亡批量删除策略过激或反过来从不清理。

处理：

1. 确认运行镜像包含 SSO 会话探针修复（clearance `ClearanceOrigin` = accounts 端点；CF 时 scheduler 回退 ListModels）。见 [operations.md · SSO](./operations.md#sso-账号校验与号池清理)。
2. 先 `--dry-run` 跑 `account sso-sweep`，看 `ok` / `session_invalid` / `cf` 比例是否合理。
3. 防封版检查 proxy + Byparr clearance；必要时降 `concurrency`。
4. 仅对 **session invalid / local JWT expired / invalid credentials** 做删除；CF soft-fail 保留并重试。
5. 用 `python3 scripts/account_health.py` 看 `sso_validation_*` fail reason 分布，再决定是否 `cleanup_bad_accounts.py`。

## 号池「扫完了吗」

快速判断：

```bash
docker cp gork:/app/data/accounts.db /tmp/accounts.db
python3 scripts/sso_sweep_progress.py /tmp/accounts.db --minutes 30
```

- `validated_ok` + `sso_fail_markers` 接近 total，且近窗口 `updated≈0` → 批量扫号已停或已完成。
- live active 中仍有大量无 `ext.sso_validation` → 未扫完，补 `sso-sweep` 或开 scheduler。
- 近窗口仍有大量 `sso_validation_session` 更新 → 仍在淘汰或运行时删号，不一定是全量 sweep。

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
