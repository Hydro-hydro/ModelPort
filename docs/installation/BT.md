# 宝塔面板部署教程

本文档提供使用宝塔面板 Docker 功能从当前仓库构建并部署 ModelPort 的步骤。

> 部署边界：ModelPort 只支持当前版本的新库初始化，不提供旧 New API 数据库迁移或历史数据兼容。请使用新的数据目录完成首次部署，不要把旧实例的数据目录直接挂载到新部署中。

> 📖 官方文档：[宝塔面板部署](https://docs.newapi.pro/zh/docs/installation/deployment-methods/bt-docker-installation)

***

## 前置要求

| 项目    | 要求                                 |
| ----- | ---------------------------------- |
| 宝塔面板  | ≥ 9.2.0 版本                         |
| 推荐系统  | CentOS 7+、Ubuntu 18.04+、Debian 10+ |
| 服务器配置 | 至少 1 核 2G 内存                       |

***

## 步骤一：安装宝塔面板

1. 前往 [宝塔面板官网](https://www.bt.cn/new/download.html) 下载适合您系统的安装脚本
2. 运行安装脚本安装宝塔面板
3. 安装完成后，使用提供的地址、用户名和密码登录宝塔面板

***

## 步骤二：安装 Docker

1. 登录宝塔面板后，在左侧菜单栏找到并点击 **Docker**
2. 首次进入会提示安装 Docker 服务，点击 **立即安装**
3. 按照提示完成 Docker 服务的安装

***

## 步骤三：安装 ModelPort

### 方法一：使用宝塔应用商店（仅适用于提供当前 ModelPort 构建的应用条目）

1. 在宝塔面板 Docker 功能中，点击 **应用商店**
2. 搜索并找到 **ModelPort**；如果应用商店没有当前版本条目，请使用下面的源码 Compose 方法
3. 点击 **安装**
4. 配置以下基本选项：
   - **容器名称**：可自定义，默认为 `modelport`
   - **端口映射**：默认为 `3000:3000`
   - **环境变量**：
     - `SESSION_SECRET`：会话密钥（**必填**，多机部署时必须一致）
     - `CRYPTO_SECRET`：加密密钥（使用 Redis 时必填）
5. 点击 **确认** 开始安装
6. 等待安装完成后，访问 `http://您的服务器IP:3000` 即可使用

### 方法二：使用 Docker Compose（推荐）

1. 在宝塔面板中创建网站目录，如 `/www/wwwroot/modelport`
2. 将当前 ModelPort 仓库上传或克隆到该目录，并创建 `docker-compose.yml` 文件：

```yaml
services:
  modelport:
    build:
      context: .
      dockerfile: Dockerfile
    image: modelport:local
    container_name: modelport
    restart: unless-stopped
    ports:
      - "3000:3000"
    volumes:
      - ./data:/data
      - ./logs:/app/logs
    environment:
      - SESSION_SECRET=${SESSION_SECRET:?请先设置随机 SESSION_SECRET}
      - CRYPTO_SECRET=${CRYPTO_SECRET:-}
      - TZ=Asia/Shanghai
```

默认 Compose 使用 SQLite，不启动 PostgreSQL 或 Redis；如需外部服务，使用环境变量传入 `SQL_DSN`、`LOG_SQL_DSN` 和 `REDIS_CONN_STRING`。
本项目只支持全新部署，不支持旧 New API 数据库迁移；请使用新的 `./data` 目录完成初始化。

3. 在终端中进入目录并启动：

```bash
cd /www/wwwroot/modelport
docker compose up -d --build
```

以上命令适用于由当前 ModelPort 版本初始化、且仍受当前版本支持的数据目录。更新过程只重新构建当前仓库源码，不会迁移旧 New API 数据库；如果数据目录来自旧实例，请创建新的 `./data` 目录并重新完成初始化。

***

## 配置说明

### 必要环境变量

| 变量名                 | 说明                 | 是否必填   |
| ------------------- | ------------------ | ------ |
| `SESSION_SECRET`    | 会话密钥，多机部署必须一致      | **必填** |
| `CRYPTO_SECRET`     | 加密密钥，使用 Redis 时必填  | 条件必填   |
| `SQL_DSN`           | 数据库连接字符串（使用外部数据库时） | 可选     |
| `REDIS_CONN_STRING` | Redis 连接字符串        | 可选     |

### 生成随机密钥

```bash
# 生成 SESSION_SECRET
openssl rand -hex 16

# 或使用 Linux 命令
head -c 16 /dev/urandom | xxd -p
```

***

## 常见问题

### Q1：无法访问 3000 端口？

1. 检查服务器防火墙是否开放 3000 端口
2. 在宝塔面板 **安全** 中放行 3000 端口
3. 检查云服务器安全组是否开放端口

### Q2：登录后提示会话失效？

确保设置了 `SESSION_SECRET` 环境变量，且值不为空。

### Q3：数据如何持久化？

使用 Docker 卷映射数据目录：

```yaml
volumes:
  - ./data:/data
```

### Q4：如何更新当前源码版本？

```bash
# 在仓库目录更新源码后重新构建
git pull
docker compose up -d --build
```

***

## 相关链接

- [官方文档](https://docs.newapi.pro/zh/docs/installation)
- [环境变量配置](https://docs.newapi.pro/zh/docs/installation/config-maintenance/environment-variables)
- [常见问题](https://docs.newapi.pro/zh/docs/support/faq)
- [GitHub 仓库](https://github.com/QuantumNous/new-api)

***

## 截图示例

![宝塔面板 Docker 安装](https://github.com/user-attachments/assets/7a6fc03e-c457-45e4-b8f9-184508fc26b0)

> ⚠️ 注意：密钥为环境变量 `SESSION_SECRET`，请务必设置！
