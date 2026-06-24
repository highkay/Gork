# Security Guide

Gork 会处理 Grok token、SSO、Cloudflare cookie、API key 和本地媒体文件。生产部署应把它当作持有高敏感凭证的服务来运行。

## Secrets and Keys

- `app.api_key` 控制 `/v1/*` API 访问；生产环境必须设置。
- `app.app_key` 控制 Admin 页面；首次启动为空会自动生成，部署后应改成强密码。
- `app.webui_key` 控制 WebUI；如果启用 WebUI，建议设置独立密码。
- SSO token、`cf_clearance`、代理凭证、数据库 DSN 不应提交到 Git。
- `.env`、`data/config.toml`、数据库备份和日志归档都应按敏感文件处理。

## Admin and WebUI Auth

- `/admin/*` 使用 `app.app_key`。
- `/webui/*` 需要 `app.webui_enabled=true`；`app.webui_key` 为空时不会额外要求密码。
- 不建议把 Admin 暴露到公网；需要公网访问时，应叠加反向代理 Basic Auth、IP allowlist 或 VPN。

## API Auth and CORS

- `/v1/*` 使用 Bearer token；`app.api_key` 为空会关闭 API key 校验，只适合本地测试。
- CORS 分为 API origin 和 Web origin，按 `security.cors.*` 配置控制。
- 浏览器跨域调用时，只允许实际需要的前端域名，不要使用宽泛通配。

## Media URL Safety

- 图片/视频本地代理 URL 可能暴露生成结果，生产环境建议启用签名 URL 和合理 TTL。
- `app.app_url` 必须是 HTTPS 公网地址，否则外部客户端可能拉取失败或降级为不安全 HTTP。
- 本地媒体缓存会保存生成文件，启用磁盘上限并定期清理更适合多人或公网环境。

## Logging and Redaction

- access log 默认记录 method、脱敏 path、status、duration 和 request id。
- 不要把完整 token、cookie、SSO、Authorization header 粘贴到 issue 或日志。
- 调试上游错误时只保留状态码、分类、截断摘要和 request id。

## Reverse Proxy and TLS

生产环境建议：

- 使用 Nginx、Caddy、Traefik 或云负载均衡终止 TLS。
- 反代时保留 `Host`、`X-Forwarded-For`、`X-Forwarded-Proto`。
- 对流式响应关闭 proxy buffering，并把 read timeout 设置到 600s 或更高。
- 只在内网或受保护环境开启 `/metrics` 和 `/debug/pprof/*`。

## Container Hardening

- Compose 默认使用只读根文件系统和 `no-new-privileges`。
- 只允许 `/app/data`、`/app/logs` 和临时目录可写。
- 防封版的 WARP 服务需要额外网络权限，应只在确实需要时启用。

