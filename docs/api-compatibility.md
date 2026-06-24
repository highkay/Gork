# API Compatibility

本文说明当前兼容面和已知限制。接口目标是兼容常见 OpenAI/Anthropic SDK 调用形态，但底层仍是 Grok Web 与 console.x.ai 的私有协议，不能保证与官方公开 API 完全等价。

## OpenAI-compatible API

| Endpoint | Auth | 兼容程度 | 说明 |
| :-- | :-- | :-- | :-- |
| `GET /v1/models` | API key | 高 | 返回可用模型列表，包含付费账号和 `*-console` 免费账号模型。 |
| `POST /v1/chat/completions` | API key | 高 | 支持流式/非流式、工具调用、图像输入、usage 统计和账号池重试。 |
| `POST /v1/responses` | API key | 中 | 转换为 Grok 对话流，覆盖常见文本、工具和流式场景。 |
| `POST /v1/images/generations` | API key | 中 | 支持多种返回格式；本地 URL 依赖 `app.app_url` 和媒体缓存配置。 |
| `POST /v1/images/edits` | API key | 中 | 兼容 multipart 和 JSON 形态，最终能力受 Grok 上游限制。 |
| `POST /v1/videos` | API key | 中 | 支持文生视频和图生视频任务提交。 |
| `GET /v1/videos/{id}` | API key | 中 | 查询视频任务状态。 |
| `GET /v1/files/video` | Public signed route | 中 | 返回视频代理内容，建议在公网启用签名 URL。 |
| `GET /v1/files/image` | Public signed route | 中 | 返回图片代理内容，建议在公网启用签名 URL。 |

已知限制：

- Grok Web 上游字段可能变化，部分响应字段为兼容层推导。
- 图片、视频生成的最终质量、耗时、失败原因由 Grok 上游决定。
- 本地媒体 URL 必须配置公网可访问的 `app.app_url`，否则外部客户端可能无法拉取资源。
- `stream=true` 时会尽量输出 OpenAI SSE 形态，但上游断流或挑战会映射为兼容错误。

## Anthropic-compatible API

| Endpoint | Auth | 兼容程度 | 说明 |
| :-- | :-- | :-- | :-- |
| `POST /v1/messages` | API key | 中 | 支持常见 messages 输入、流式输出和工具调用转换。 |

已知限制：

- Anthropic 的完整 tool_choice、content block 和停止原因语义会映射到 Grok 能力范围内。
- 多模态输入取决于底层 Grok 模型是否支持。

## Admin and WebUI private API

Admin/WebUI API 是内部页面协议，不承诺第三方稳定兼容。当前覆盖：

- `/admin/api/config`：读取、更新、重置运行时配置。
- `/admin/api/status`：运行状态、存储后端、观测开关、缓存摘要。
- `/admin/api/tokens*`：账号分页、导入、编辑、禁用、池调整。
- `/admin/api/batch/*`：批量刷新、NSFW、缓存清理等后台任务。
- `/admin/api/cache*`：本地媒体缓存统计、列表、删除、清理。
- `/webui/api/verify`：WebUI 登录状态检查。

使用建议：

- 第三方集成优先使用 `/v1/*`。
- Admin/WebUI 私有 API 只用于同版本前端页面，不建议作为外部自动化合同。
- 如果必须自动化 Admin API，先固定镜像 tag 或 digest，并在升级前跑 smoke 测试。

