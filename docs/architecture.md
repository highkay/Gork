# Architecture

Gork 是一个 Go 单体服务，按职责分为入口、控制面、数据面、平台层和产品适配层。运行时请求从 `cmd/gork` 启动，进入 `app` 组装的 HTTP handler，再分发到 OpenAI、Anthropic、Admin、WebUI 等产品路由。

## 分层职责

| 层 | 目录 | 职责 |
| :-- | :-- | :-- |
| 入口 | `cmd/gork` | 解析命令、启动 HTTP 服务、提供 `config validate/docs` 与健康检查命令。 |
| 应用组装 | `app` | 加载配置、初始化生命周期 hook、注册产品路由、挂载观测和安全中间件。 |
| 控制面 | `app/control` | 管理账号、模型、代理目录和后台可变状态，不直接处理具体 API 协议。 |
| 数据面 | `app/dataplane` | 执行账号选择、反向协议、SSE/WS/gRPC-web 解析、上游传输与反馈。 |
| 平台层 | `app/platform` | 配置、鉴权、日志、错误、观测、runtime store、存储、启动迁移等通用能力。 |
| 产品层 | `app/products` | 暴露 OpenAI、Anthropic、WebUI/Admin 兼容接口，负责请求/响应转换。 |
| 静态资源 | `app/statics` | Admin/WebUI 静态页面、CSS、JS、i18n 资源。 |

## 请求流

1. `cmd/gork` 创建 HTTP server，并调用 `app.Handler()`。
2. `app` 加载 `config.defaults.toml`、持久化配置后端和环境变量覆盖。
3. HTTP 中间件处理 request id、CORS、安全头、鉴权、日志和可观测性。
4. `/v1/*` 请求进入产品路由，由产品层解析 OpenAI/Anthropic 兼容参数。
5. 产品层调用账号选择、代理目录、反向协议和上游传输。
6. 数据面把 Grok web/console 响应转换为 OpenAI/Anthropic 兼容 JSON 或 SSE。
7. Admin/WebUI 路由通过控制面读写账号、配置、缓存和后台任务。

## 边界规则

- `app/platform` 不依赖 `app/products`，保持基础设施可复用。
- `app/control` 不依赖 WebUI/Admin 路由，业务状态管理独立于页面实现。
- `app/dataplane` 不依赖 `app/products/web`，避免核心协议层被后台页面绑定。
- 产品层可以组合控制面、数据面和平台层，但不应把协议解析逻辑下沉到平台层。

## 配置与状态

- 默认配置来自 `config.defaults.toml`。
- 运行时配置持久化到本地 TOML、Redis、MySQL 或 PostgreSQL，具体由启动期环境变量决定。
- `GROK_` 前缀变量覆盖运行时配置 key，例如 `GROK_APP_API_KEY` 覆盖 `app.api_key`。
- 账号存储和 runtime store 是不同概念：账号存储保存 token/配额/状态，runtime store 保存任务快照和调度锁。

