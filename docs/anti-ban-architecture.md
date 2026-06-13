<div align="center">

# Grok2API 防封版本技术解析

**如何在 Cloudflare 高风控环境下稳定访问 grok.com**

</div>

---

## 为什么需要防封？

grok.com 前端部署在 Cloudflare 之后，具有多层反爬机制：

| 检测层 | 检测手段 | 被封表现 |
|--------|---------|---------|
| **IP 风控** | 出口 IP 信誉评分，数据中心 IP 直接拒绝 | 403 Forbidden |
| **TLS 指纹** | JA3/JA4 指纹识别，非浏览器 TLS 握手特征 | 403 或静默拦截 |
| **JS 挑战** | Cloudflare Turnstile / JS Challenge | 需要 `cf_clearance` cookie |
| **浏览器指纹** | Client Hints、Sec-CH-UA 一致性校验 | 403 或降级服务 |
| **设备指纹** | `x-statsig-id` Statsig SDK 设备标识 | 403 或关联封禁 |

传统的 `requests` 或 `aiohttp` 库在以上任何一层都会被拦截。防封版本针对每一层都有对应的解决方案。

---

## 架构总览

```
                    ┌──────────────────────────────────────────────┐
                    │                grok2api 主服务                │
                    │                                              │
                    │  ┌─────────┐  ┌──────────┐  ┌────────────┐  │
                    │  │curl-cffi│  │ statsig  │  │cf_clearance│  │
                    │  │TLS 伪装 │  │ 指纹生成 │  │ cookie 注入│  │
                    │  └────┬────┘  └──────────┘  └─────┬──────┘  │
                    │       │                           │          │
                    │       └──────────┬────────────────┘          │
                    │                  │                            │
                    └──────────────────┼────────────────────────────┘
                                       │
                              ┌────────▼────────┐
                              │    Privoxy       │
                              │  HTTP → SOCKS5   │
                              │   :8118          │
                              └────────┬─────────┘
                                       │
                              ┌────────▼─────────┐
                              │   WARP Proxy     │
                              │  SOCKS5 :1080    │
                              │  Cloudflare IP   │
                              └────────┬─────────┘
                                       │
                              ┌────────▼─────────┐
                              │ Cloudflare WARP  │
                              │    网络出口       │
                              └────────┬─────────┘
                                       │
                              ┌────────▼─────────┐     ┌──────────────┐
                              │    grok.com      │     │ FlareSolverr │
                              │  Cloudflare CDN  │◄────│  自动解挑战   │
                              └──────────────────┘     │  获取 cookie  │
                                                       └──────────────┘
```

---

## 第一层：干净 IP — Cloudflare WARP

### 问题

数据中心 IP（如 AWS、GCP、阿里云）的信誉评分极低，Cloudflare 会直接拦截这些 IP 的请求，甚至不给 JS 挑战的机会。

### 解决方案

使用 Cloudflare 自己的 WARP VPN 服务作为出口代理。

```
你的服务器 → WireGuard 隧道 → Cloudflare 边缘网络 → grok.com
```

**核心原理**：WARP 分配的 IP 属于 Cloudflare 自己的 IP 池。Cloudflare 不会封自己的 IP——这些 IP 的信誉评分天然是最高的。

WARP 容器通过 Docker 的 `NET_ADMIN` + `SYS_MODULE` 权限创建 WireGuard 隧道接口，对外暴露 SOCKS5 代理端口 `1080`。

### 为什么需要 Privoxy？

grok2api 使用的 `curl-cffi` 库和 `FlareSolverr` 对 HTTP 代理的支持比 SOCKS5 更稳定。Privoxy 作为中间层，将 HTTP 请求透明转发到 WARP 的 SOCKS5 端口：

```
curl-cffi → HTTP → Privoxy:8118 → SOCKS5 → WARP:1080 → Cloudflare 网络
```

配置极简（两行）：
```
listen-address 0.0.0.0:8118
forward-socks5 / warp-proxy:1080 .
```

---

## 第二层：TLS 指纹伪装 — curl-cffi

### 问题

Cloudflare 会检查 TLS Client Hello 中的指纹特征（JA3/JA4），包括：
- 支持的密码套件列表及顺序
- TLS 扩展列表及顺序
- 椭圆曲线参数
- HTTP/2 SETTINGS 帧
- ALPN 协商顺序

Python 的 `requests`、`aiohttp` 等库的 TLS 指纹与浏览器差异巨大，一眼就能被识别为非浏览器流量。

### 解决方案

使用 `curl-cffi` 库，它可以在 TLS 层面完整模拟真实浏览器的指纹：

```python
session_kwargs = {
    "impersonate": "chrome136",  # 模拟 Chrome 136 的完整 TLS 指纹
}
async with ResettableSession(**session_kwargs) as session:
    response = await session.post("https://grok.com/rest/app-chat/conversations/new", ...)
```

`impersonate="chrome136"` 会让底层 curl 发出与真实 Chrome 136 完全一致的 TLS Client Hello，包括密码套件顺序、扩展列表、ALPN 协商等所有细节。

### 浏览器版本自动匹配

系统会从 FlareSolverr 返回的 User-Agent 中自动提取浏览器版本，确保 UA 字符串和 TLS 指纹一致：

```python
def browser_from_user_agent(user_agent: str) -> str:
    if "chrome/136" in ua:   return "chrome136"
    if "chrome/131" in ua:   return "chrome131"
    if "firefox/123" in ua:  return "firefox123"
    if "edg/131" in ua:      return "edge131"
```

如果 UA 声称自己是 Chrome 136，但 TLS 指纹像 Python——Cloudflare 会立即检测到这种不匹配并拦截。

---

## 第三层：Cloudflare 挑战自动解决 — FlareSolverr

### 问题

即使有了干净 IP 和正确的 TLS 指纹，Cloudflare 仍可能弹出 JS 挑战页面，要求证明"你是人类"。通过挑战后会颁发一个 `cf_clearance` cookie，后续请求必须携带这个 cookie 才能正常访问。

### 解决方案

FlareSolverr 是一个独立服务，内置无头 Chrome 浏览器，可以自动解决 Cloudflare 的 JS 挑战：

```
grok2api                    FlareSolverr
   │                            │
   │  POST /v1                  │
   │  {cmd: "request.get",      │
   │   url: "https://grok.com", │
   │   proxy: {url: "..."}}     │
   │ ─────────────────────────► │
   │                            │  启动无头 Chrome
   │                            │  通过 Privoxy→WARP 访问 grok.com
   │                            │  自动解 JS 挑战
   │                            │  等待 cf_clearance cookie 出现
   │                            │
   │  {solution: {              │
   │    cookies: [...],         │
   │    userAgent: "..."}}      │
   │ ◄───────────────────────── │
   │                            │
   │  提取 cf_clearance         │
   │  缓存为 ClearanceBundle    │
```

### 缓存与刷新

获取到的 `cf_clearance` 会被缓存为 `ClearanceBundle`，默认每 3600 秒自动刷新。刷新时采用"先获取新的，成功后才替换旧的"策略，确保 FlareSolverr 临时不可用时旧凭证仍然有效。

多个并发请求同时需要刷新时，只有一个协程会实际调用 FlareSolverr（单飞机制），其他协程等待结果，避免重复请求。

---

## 第四层：浏览器指纹模拟 — 请求头伪装

### 完整的请求头

每一个发往 grok.com 的请求都携带完整的浏览器请求头：

| 请求头 | 作用 | 示例值 |
|--------|------|--------|
| `User-Agent` | 浏览器身份标识 | `Mozilla/5.0 ... Chrome/136.0.0.0 ...` |
| `Sec-Ch-Ua` | Client Hints 浏览器标识 | `"Google Chrome";v="136", "Chromium";v="136"` |
| `Sec-Ch-Ua-Platform` | 操作系统 | `"macOS"` |
| `Sec-Fetch-Site` | 请求来源类型 | `same-origin` |
| `Sec-Fetch-Mode` | 请求模式 | `cors` |
| `Origin` / `Referer` | 页面来源 | `https://grok.com` |
| `Cookie` | 认证 + CF 通行证 | `sso=...; cf_clearance=...` |
| `x-statsig-id` | Statsig SDK 设备指纹 | `eDpUeXBlRXJyb3I6...`（Base64） |
| `x-xai-request-id` | 前端请求 ID | UUID v4 |
| `Baggage` | Sentry 监控追踪信息 | `sentry-environment=production,...` |

### Sec-CH-UA 一致性

Client Hints 必须与 User-Agent 保持一致。系统会从 UA 中提取浏览器版本号，生成匹配的 Sec-CH-UA 头：

```
UA:         Chrome/136.0.0.0
Sec-Ch-Ua:  "Google Chrome";v="136", "Chromium";v="136", "Not(A:Brand";v="24"
TLS 指纹:   impersonate=chrome136
```

三者保持一致，Cloudflare 无法通过交叉验证发现异常。

---

## 第五层：设备指纹 — x-statsig-id

### 问题

grok.com 前端集成了 Statsig SDK（A/B 测试服务），每个浏览器会话会生成一个 `x-statsig-id` 设备标识。如果所有请求共享同一个 statsig-id，Cloudflare/xAI 可以将这些请求关联起来，识别为同一个自动化客户端。

### 解决方案

动态模式下，每次请求随机生成不同的 statsig-id。生成的格式模拟 Statsig SDK 初始化失败时的错误上报格式：

```python
# 随机选择两种错误模板之一
msg = f"x1:TypeError: Cannot read properties of null (reading 'children['{random_string}']')"
# 或
msg = f"x1:TypeError: Cannot read properties of undefined (reading '{random_string}')"

# Base64 编码
statsig_id = base64.b64encode(msg.encode()).decode()
```

每次请求的 `random_string` 都不同，使得每个请求看起来像来自不同的浏览器会话。

---

## 第六层：403 自动恢复 — 三层重试

即使做了以上所有伪装，仍然可能偶尔被 Cloudflare 拦截（IP 轮转、cf_clearance 过期等）。防封版本有三层自动恢复机制：

### 第一层：Transport 层 — 重建连接

收到 403 后，`ResettableSession` 自动在下一次请求前重建 TCP/TLS 连接：

```python
class ResettableSession:
    async def _request(self, method, *args, **kwargs):
        await self._maybe_reset()           # 如果上次 403，先重建 Session
        response = await self._session.post(...)
        if response.status_code == 403:
            self._reset_pending = True       # 标记下次重建
        return response
```

新建的 Session 会产生全新的 TLS 指纹和 TCP 连接，相当于"换了一个浏览器"。

### 第二层：Control 层 — 刷新 Clearance

403 反馈到代理控制层后，当前的 `ClearanceBundle` 被标记为无效：

```
403 响应 → feedback(CHALLENGE) → bundle.state = INVALID → 下次请求触发 FlareSolverr 重新获取 cf_clearance
```

### 第三层：App 层 — 换号重试

如果是账号级别的问题（429 限频、401 凭证失效），App 层会自动切换到另一个 SSO 账号重试。

---

## 一键部署

以上所有技术细节都已封装为 Docker Compose，用户无需了解底层原理：

```bash
git clone https://github.com/jiujiu532/grok2api
cd grok2api/grok2api-main/grok2api-main
docker compose -f docker-compose.warp.yml up -d
```

`init-config` 容器会自动写入所有代理配置，启动后进入 Admin 后台添加账号即可使用。

---

## 与标准版的对比

| 维度 | 标准版 | 防封版 |
|------|--------|--------|
| 出口 IP | 服务器原始 IP | Cloudflare WARP IP |
| TLS 指纹 | curl-cffi 伪装 | curl-cffi 伪装 |
| cf_clearance | 手动配置或不配置 | FlareSolverr 自动获取 + 定时刷新 |
| 403 恢复 | 重建连接 | 重建连接 + 自动刷新 clearance |
| 适用场景 | IP 干净、无 CF 拦截 | IP 被风控、需要稳定访问 |
| 资源占用 | 低（单容器） | 中（5 个容器） |

---

<div align="center">

**防封版本的核心理念：让每一个请求都像是从真实浏览器发出的。**

从 IP 地址、TLS 握手、浏览器指纹、Cookie 认证到设备标识，每一层都与真实用户无异。

</div>
