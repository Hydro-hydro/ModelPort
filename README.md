# ModelPort

> 面向个人使用的 AI API 网关与协议转换器。

ModelPort 基于 [New API](https://github.com/QuantumNous/new-api) fork 改造。项目起因是实际使用 New API 时，遇到不同上游服务之间协议转换、流式响应和请求参数兼容方面的问题，因此围绕个人部署场景进行了精简与改造。

## 改造重点

- **个人化精简**：减少个人用户通常用不到的平台化配置与管理复杂度。
- **协议转换**：围绕 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Google Gemini 等常见协议，提供统一接入与转换能力。
- **兼容性优化**：重点关注流式响应、工具调用、思考内容、Usage 和错误信息在转换过程中的一致性。
- **统一入口**：通过渠道、模型和访问令牌管理，将多个上游模型服务收敛到一个 API 入口。

> 具体支持范围会随代码版本变化，请以当前版本的渠道配置和实现为准。

## 快速开始

### 使用 Docker

```bash
export SESSION_SECRET="$(openssl rand -hex 32)"
export CRYPTO_SECRET="$(openssl rand -hex 32)"
docker compose up -d --build
```

首次启动前必须设置这两个密钥，并在后续重启时继续使用相同的值。建议将它们保存到项目根目录的 `.env` 文件或其他受保护的环境变量管理方式中；默认 Compose 会在缺少任一密钥时直接报错，不会使用临时随机值启动。

启动后访问 <http://localhost:3000>，按页面提示完成初始化。

默认 Compose 配置直接构建当前仓库，并使用挂载到 `./data` 的 SQLite 数据库，
不依赖 PostgreSQL、Redis 或其他外部服务。需要使用其他数据库或缓存时，
请在启动前通过环境变量配置 `SQL_DSN`、`LOG_SQL_DSN` 或 `REDIS_CONN_STRING`。

生产环境必须显式设置持久、随机且妥善保管的 `SESSION_SECRET` 和 `CRYPTO_SECRET`。
更换任一密钥会导致浏览器会话和依赖该密钥的数据失效。
不要将 `SESSION_SECRET` 设置为 `random_string`。

本项目面向全新部署，不提供旧 New API 数据库迁移或历史数据兼容；使用旧实例时请创建新的数据目录并重新完成初始化。

任务插件、媒体任务、部署、多节点和系统维护任务默认关闭。可以在后台“系统设置 → Operations → Optional Features”中保存开关，修改会在下次重启后生效；也可以在首次启动前显式设置对应环境变量：

```bash
MODELPORT_ENABLE_TASK_PLUGINS=true
MODELPORT_ENABLE_MEDIA_TASKS=true
MODELPORT_ENABLE_DEPLOYMENTS=true
MODELPORT_ENABLE_MULTI_NODE=true
MODELPORT_ENABLE_SYSTEM_TASKS=true
```

任务插件或媒体任务会自动启用其依赖的系统任务表和轮询 runner；关闭能力不会删除已有可选表。
保存到当前数据库的 `feature.*` 配置会在后续启动时优先于同名环境变量；如果没有保存配置，环境变量只作为首次启动默认值。

### 本地开发

```bash
cd web
bun install
VITE_REACT_APP_VERSION=$(cat ../VERSION) bun run build
cd ..
GOWORK=off go run main.go
```

默认监听 `3000` 端口，可通过 `PORT` 或 `--port` 调整。更多配置见 [`.env.example`](./.env.example)。

## 适用场景

- 个人自建 AI API 聚合服务
- 需要在 OpenAI、Claude、Gemini 等协议之间做适配的客户端
- 希望用一个兼容入口管理多个模型渠道的本地部署

## 上游项目与许可

ModelPort 是基于 [New API](https://github.com/QuantumNous/new-api) 的个人化改造版本，保留原项目及 QuantumNous 的相关归属信息。

本项目遵循 [GNU Affero General Public License v3.0](./LICENSE)。修改版本请保留以下归属要求：

- `Frontend design and development by New API contributors.`
- 用户界面中保留原项目链接：<https://github.com/QuantumNous/new-api>
- 原项目文档：<https://docs.newapi.pro/>
- 原项目发布页：<https://github.com/QuantumNous/new-api/releases>
- 原项目 Docker 镜像：`calciumion/new-api`

如需商业授权，请联系 <support@quantumnous.com>。

相关项目：[new-api-key-tool](https://github.com/Calcium-Ion/new-api-key-tool)、[new-api-horizon](https://github.com/Calcium-Ion/new-api-horizon)。

## 免责声明

请仅在合法、获得授权的前提下使用本项目，并遵守上游服务商的服务条款以及适用的法律法规。
