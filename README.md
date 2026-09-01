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
docker build -t modelport .
docker run -d \
  --name modelport \
  --restart unless-stopped \
  -p 3000:3000 \
  -v "$(pwd)/data:/data" \
  modelport
```

启动后访问 <http://localhost:3000>，按页面提示完成初始化。

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
