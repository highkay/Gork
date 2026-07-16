# Development & Release Workflow

本文定义本仓库的**正确开发与发布链路**。Agent 与人类贡献者默认遵循此文档；偏离前需有明确理由。

## 原则

1. **镜像只由 GitHub Actions 构建并推送到 GHCR。**  
   禁止用本机 `docker build` / `docker push` 替代正式发布（紧急热修且无法等 CI 时除外，且事后仍应用 Actions 产物对齐）。
2. **代码先本地测，再提交并推送。**  
   推送到部署用远程的 `main` 后，等待 Actions 完成，再用 `sha-<commit>` 拉镜像升级。
3. **生产固定 `sha-<commit>` tag，不用漂移的 `latest` 裸跑。**

## 仓库与镜像对应关系

| 角色 | 说明 |
| :-- | :-- |
| 上游参考 | `origin` → `DSLZL/Gork`（可跟上游，不必然是本机部署源） |
| 本机部署常用远程 | `highkay` → `highkay/Gork` |
| GHCR 镜像名 | 与 **触发 Actions 的 GitHub 仓库** 绑定：`ghcr.io/<owner>/gork` |
| 本机当前示例 | 推 `highkay/Gork` 的 `main` → 产物 `ghcr.io/highkay/gork:sha-<shortsha>` |

Actions 用 `github.repository` / `github.repository_owner` 决定镜像路径（见 `.github/workflows/docker.yml`、`build.yml`）。  
推到哪个 GitHub 仓库，就出哪个命名空间的 GHCR 包。

## 正确链路（默认）

```text
改代码
  → 本地 go test（相关包或 go test ./...）
  → git commit
  → git push <部署远程> main          # 例如: git push highkay main
  → 等待 GitHub Actions 成功
       • docker.yml / Build Docker Image  （多架构 + sha-/latest 等）
       • 或 build.yml / Build and Push Image
  → 确认 GHCR 已有 ghcr.io/<owner>/gork:sha-<commit>
  → 改 .env: GORK_IMAGE=ghcr.io/<owner>/gork:sha-<commit>
  → docker compose -f <实际 compose> pull gork
  → docker compose -f <实际 compose> up -d gork
  → 验证 /health、/meta、关键业务 smoke
  → 需要时再开运维监控（SSO 扫号、日志 403/429 等）
```

### 1. 本地开发与测试

```bash
# 单元/包测试（改动相关优先）
go test ./app/control/account/ ./app/dataplane/reverse/protocol/ ./app/dataplane/reverse/transport/ ./cmd/gork/

# 需要更完整时
go test ./...
```

- 本地可 `go build -o /tmp/gork ./cmd/gork` 做 CLI smoke。  
- **本地 build 的二进制或镜像不用于替换生产 GHCR 发布流程。**

### 2. 提交与推送

```bash
git status
git add <相关文件>          # 不要提交 .env、config.toml 密钥、*.db
git commit -m "..."
git push highkay main       # 以实际部署远程为准
```

推送后到 GitHub 查看：

- Actions → 对应 workflow 是否 **success**
- Packages → `gork` 是否出现新 tag `sha-<commit>`

不要在 CI 未成功时改生产 `GORK_IMAGE` 硬拉不存在的 tag。

### 3. Actions 构建产物

| Workflow | 触发 | 典型 tag |
| :-- | :-- | :-- |
| `docker.yml` (`Build Docker Image`) | push `main` / tag `v*` | `latest`、`sha-<sha>`、分支/semver（多架构） |
| `build.yml` (`Build and Push Image`) | push `main` / `workflow_dispatch` | `latest`、短 sha（常见 `linux/amd64`） |

生产建议：

```bash
# 与已推送 commit 对齐
git rev-parse --short HEAD   # 例如 30be5f1
# .env
GORK_IMAGE=ghcr.io/highkay/gork:sha-30be5f1
```

（若 `build.yml` 的 metadata 未加 `sha-` 前缀，以 Packages 页面实际 tag 为准。）

### 4. 部署升级（本机 warpplus 示例）

本环境常用：

```bash
# /home/admin/Gork/.env
GORK_IMAGE=ghcr.io/highkay/gork:sha-<commit>

docker compose -f docker-compose.warpplus.yml pull gork
docker compose -f docker-compose.warpplus.yml up -d gork

curl -sS http://127.0.0.1:${HOST_PORT:-8008}/health
docker logs gork --since 5m
```

回滚：

```bash
# 改回上一成功 tag
GORK_IMAGE=ghcr.io/highkay/gork:sha-<previous>
docker compose -f docker-compose.warpplus.yml pull gork
docker compose -f docker-compose.warpplus.yml up -d gork
```

更多运维见 [operations.md](./operations.md)。

## 明确禁止 / 避免

| 做法 | 原因 |
| :-- | :-- |
| 本机 `docker build && docker push ghcr.io/...` 当常规发布 | 跳过 CI 测试与统一 metadata/SBOM；与仓库 Actions 双轨混乱 |
| 未 push 就改生产镜像 | GHCR 无对应 commit 产物 |
| 生产长期跟 `latest` 不记 sha | 无法复现与回滚 |
| 提交 `.env`、明文 token、本地 `config.toml` 密钥 | 安全风险 |

## Agent 操作清单

在被要求「提交并更新容器」时，应按序：

1. 本地相关测试通过  
2. `git commit`（不含密钥）  
3. `git push` 到部署远程 `main`  
4. **等待** GitHub Actions 成功（查 Actions / Packages，而不是本机 build）  
5. 更新 `.env` 的 `GORK_IMAGE=...:sha-<commit>`  
6. `compose pull` + `up -d`  
7. 健康检查与业务/扫号监控  

仅当用户**明确要求**本地紧急热修镜像，且无法使用 Actions 时，才本机构建；并在响应中说明这是例外。

## 相关文件

| 文件 | 用途 |
| :-- | :-- |
| `.github/workflows/docker.yml` | 主镜像多架构构建推送 |
| `.github/workflows/build.yml` | 另一套 build+push（含 `workflow_dispatch`） |
| `.github/workflows/ci.yml` | CI 测试 |
| `docker-compose.yml` | 标准版 |
| `docker-compose.warp.yml` / `docker-compose.warpplus.yml` | 防封版 |
| [operations.md](./operations.md) | 部署、升级、备份 |
| [troubleshooting.md](./troubleshooting.md) | 排障 |

## 本地调试命令（不替代发布）

```bash
# 配置校验
go run ./cmd/gork config validate

# 账号仓储一致性
go run ./cmd/gork account check

# SSO 会话扫号（运维工具；全量清号仍建议用已部署镜像内的 scheduler / 同版本 CLI）
go run ./cmd/gork account sso-sweep --help
```
