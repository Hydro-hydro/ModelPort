# 翻译术语表 (Translation Glossary)

本文档为翻译贡献者提供个人版当前仍使用的核心术语，确保模型聚合、路由、计费和管理员界面的文案一致。

This document lists the core terminology still used by the personal edition so that model aggregation, routing, billing, and administrator UI translations stay consistent.

## 核心概念 (Core Concepts)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 倍率 | Ratio | 用于计算模型和任务价格的乘数因子 | Multiplier used to calculate model and task prices |
| 令牌 | Token | API 访问凭证，也指模型处理的文本单元 | API access credential or text unit processed by a model |
| 渠道 | Channel | 上游 API 服务提供商的接入通道 | Access channel for an upstream API provider |
| 路由分组 | Route Group | 决定渠道路由、模型可用范围和成本倍率 | Selects routing, model availability, and cost multiplier |
| 额度 | Quota | API Token 的访问上限或本次模型用量单位 | Token access limit or model-usage unit for one request |

## 模型相关 (Model Related)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 提示 | Prompt | 模型输入内容 | Model input content |
| 补全 | Completion | 模型输出内容 | Model output content |
| 输入 | Input/Prompt | 发送给模型的内容 | Content sent to the model |
| 输出 | Output/Completion | 模型返回的内容 | Content returned by the model |
| 模型倍率 | Model Ratio | 不同模型的计费倍率 | Billing ratio for different models |
| 补全倍率 | Completion Ratio | 输出内容的额外计费倍率 | Additional billing ratio for output content |
| 固定价格 | Price per call | 按次计费的价格 | Fixed price per call |
| 按量记账 | Usage-based accounting | 根据实际模型用量记录费用，不涉及钱包余额 | Records model usage without a wallet balance |
| 按次计费 | Pay-per-call | 每次调用固定价格 | Fixed price per invocation |

## 管理员账户 (Administrator Account)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| root 管理员 | Root Administrator | 个人版唯一的后台所有者 | The single dashboard owner of the personal edition |
| 管理员 Session | Administrator Session | 管理后台的登录会话 | Login session for the administrator dashboard |
| API Token | API Token | 调用 Relay 和任务接口的访问凭证 | Credential for Relay and task API calls |
| 安全证明 | Security Proof | 绑定管理员 Session 的短期敏感操作凭证 | Short-lived credential bound to an administrator Session for sensitive actions |

## 渠道管理 (Channel Management)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 密钥 | Key | 上游 API 访问密钥或渠道凭证 | Upstream API key or channel credential |
| 优先级 | Priority | 渠道选择优先级 | Channel selection priority |
| 权重 | Weight | 负载均衡权重 | Load-balancing weight |
| 代理 | Proxy | 代理服务器地址 | Proxy server address |
| 模型映射 | Model Mapping | 请求模型名到上游模型名的替换 | Replacement from the request model name to the upstream model name |
| 厂商 | Vendor | 提供模型或 API 服务的厂商 | Provider of a model or API service |

## 计费相关 (Billing Related)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 预扣 | Pre-consumption | 请求开始前暂时扣除预计额度 | Temporarily deduct estimated quota before a request starts |
| 结算 | Settlement | 按实际用量调整预扣额度 | Adjust the pre-consumed quota to actual usage |
| 退款 | Refund | 异常终态退回未使用的预扣额度 | Return unused pre-consumed quota after an error terminal state |
| 自动分组 | Auto Groups | 按配置顺序尝试可用路由分组 | Try route groups in configured order |

## 翻译注意事项 (Translation Guidelines)

- **路由分组 (Route Group)** 只表示模型路由和计费分组，不表示账户等级或用户权限。
- **额度 (Quota)** 根据上下文可以指 API Token 访问上限或模型处理用量单位，不表示个人钱包余额。
- **Token** 根据上下文可能指 API Token、Access Token 或文本 Token。
- **模型价格 (Model Price)** 是模型目录和计费配置，不表示充值、订阅或公开商业售卖价格。
- **倍率 (Ratio)** 应保持为价格计算的乘数因子。

---

**贡献说明**: 如发现术语翻译不一致或有更好的翻译建议，欢迎提交 Issue 或 Pull Request。

**Contribution Note**: If you find terminology inconsistencies or better translations, please submit an Issue or Pull Request.
