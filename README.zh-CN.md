# PlexRouter

**可自托管的 OpenAI 兼容大模型网关。** 把 OpenAI、Anthropic、Google Gemini 以及任何 OpenAI 兼容接口（DeepSeek、Groq、Mistral、Together、vLLM、Ollama……）统一到一个接口后面，发放 API Key，按 token 从预充值余额中计费——只需一个 Go 二进制文件，自带 Web 控制台。

[English](README.md)

> PlexRouter 是受 OpenRouter 等服务启发的独立开源项目，与 OpenRouter, Inc. 没有任何关联。

## 功能

- **一个接口调用所有模型** —— OpenAI 格式的 `POST /v1/chat/completions`（支持流式、工具调用、图片、JSON 模式）、`/v1/embeddings`、`/v1/models`。可直接使用官方 OpenAI SDK 及所有兼容 OpenAI 接口的工具。
- **Anthropic Messages API 与 Claude Code** —— `POST /v1/messages` 对 Anthropic 原样透传，对其他供应商自动转换，Claude Code 和 Anthropic SDK 可以使用任意模型。
- **原生协议适配** —— 自动转换为 Anthropic Messages API 和 Gemini API（含工具调用与流式）；OpenAI 兼容上游直接透传，保留额外字段。推理强度、提示词缓存（`cache_control`）和结构化输出会在不同供应商间自动转换，缓存 token 按缓存价计费。
- **路由与失败回退** —— 一个模型（如 `openai/gpt-4o`）可以由多个供应商提供，按优先级依次尝试；客户端也可以传 `models: [...]` 回退到其他模型。回退在向客户端发送第一个字节之前完成，对客户端透明。
- **余额与计费** —— 每个模型按每百万输入/输出 token 定价，每次请求按上游返回的 token 数精确扣费（纳美元精度）。支持 Key 额度上限、兑换码、管理员调账、注册赠送。
- **为运营者设计** —— 用户完全不接触供应商 Key。每个供应商可配多个上游 Key 轮流使用；被拒绝或额度用完的 Key 自动停用，被限流的 Key 暂时冷却，连续失败的线路由熔断器自动跳过。记录上游成本与收入、生成利润报表，查看供应商健康度和告警，还能从 OpenRouter 模型目录按参考价加价一键导入模型。
- **限流** —— 按用户（可设全局默认值）和按 Key 限制每分钟请求数；Key 可以限定只能调用指定模型。
- **Google / GitHub 登录** —— 无需管理密码；首次登录自动建号，按已验证邮箱关联已有账号。
- **Web 控制台** —— 用户可管理 API Key、查看用量、日志和消费图表、浏览模型列表和文档；管理员可管理供应商（支持连通性测试和一键导入模型）、模型与路由、用户、请求日志和兑换码。支持 10 种语言（English、简体中文、繁體中文、日本語、한국어、Español、Français、Deutsch、Português、Русский）及浅色/深色主题。
- **部署简单** —— 单个静态二进制，内置 SQLite（纯 Go，无需 CGO），零必填配置。提供 Docker 镜像和 docker-compose。
- **默认安全** —— 供应商 API Key 加密存储（AES-256-GCM），用户 API Key 只存哈希，不存密码（通过 Google/GitHub 登录，带 PKCE），HttpOnly 会话 Cookie，CSRF 防护，请求限流。

## 快速开始

用户通过 **GitHub 或 Google** 登录（不设密码），所以先创建一个 OAuth 应用。GitHub 最快：打开 [Settings → Developer settings → OAuth Apps → New OAuth App](https://github.com/settings/developers)，Homepage 填 `http://localhost:8080`，回调地址填 `http://localhost:8080/api/auth/oauth/github/callback`，复制 Client ID 并生成 Client Secret。

### Docker

```bash
docker run -d --name plexrouter -p 8080:8080 -v plexrouter_data:/data \
  -e PLEXROUTER_GITHUB_CLIENT_ID=<client-id> -e PLEXROUTER_GITHUB_CLIENT_SECRET=<client-secret> \
  ghcr.io/local4a/plexrouter:latest
```

或使用 compose（把这两个变量写进 `.env`）：

```bash
docker compose up -d
```

### 二进制

从 [Releases](https://github.com/local4a/plexrouter/releases) 下载对应平台的文件后直接运行：

```bash
./plexrouter
```

### 从源码构建

需要 Go 1.23+ 和 Node.js 20+。

```bash
git clone https://github.com/local4a/plexrouter.git
cd plexrouter
make build        # 构建控制台到 web/dist，构建服务端到 bin/
./bin/plexrouter
```

然后打开 <http://localhost:8080>：

1. **用 GitHub 登录** —— 第一个登录的账号自动成为管理员（也可以用 `PLEXROUTER_ADMIN_EMAILS` 指定管理员）。
2. **管理 → 供应商** —— 添加供应商（例如类型选 `openai` 填 OpenAI Key，或选 `openai_compatible` 填 `https://api.deepseek.com/v1`）。点 **测试**，再点 **导入模型**。
3. **管理 → 模型管理** —— 导入的模型默认 *禁用且价格为 0*，设置价格（美元 / 百万 token）后启用。一个模型可以配置多个供应商实现回退。
4. **管理 → 用户** —— 给自己加点余额（或生成兑换码）。
5. **API Keys** —— 创建 Key 并调用接口。

## 生产部署（单台服务器 + Docker）

任何装了 Docker 的 Linux 服务器都可以。单实例 + SQLite 足够支撑一个团队或小规模的对外服务。

```bash
git clone https://github.com/local4a/plexrouter.git && cd plexrouter
cp .env.example .env
```

编辑 `.env`：

- `PLEXROUTER_SECRET` —— 填 `openssl rand -hex 32` 的输出并妥善保存。它用来加密供应商 API Key，丢失或修改后已保存的 Key 将无法解密。
- `PLEXROUTER_GITHUB_CLIENT_ID` / `_SECRET` 和/或 `PLEXROUTER_GOOGLE_CLIENT_ID` / `_SECRET`，以及 `PLEXROUTER_PUBLIC_URL`（如 `https://api.example.com`）—— 登录配置，见 [Google / GitHub 登录](#google--github-登录)。
- `PLEXROUTER_ADMIN_EMAILS` —— 填你自己的邮箱，这样无论谁先登录，你都是管理员。
- `PLEXROUTER_ALLOW_REGISTER=false` —— 只允许已有账号（和指定的管理员）登录时设置。
- `HTTPS_PROXY` —— 服务器无法直连模型接口时（例如国内服务器访问 OpenAI / Anthropic / Google）设置出站代理；或者只接入国内模型（DeepSeek、通义千问、豆包等，类型选 `openai_compatible`）。

```bash
docker compose up -d          # 或：docker compose up -d --build（从源码构建镜像）
docker compose logs -f
```

国内服务器拉取 `ghcr.io` 镜像较慢时，可以直接用 `docker compose up -d --build` 在服务器上构建。

**HTTPS。** 在前面加一层反向代理，把 `docker-compose.yml` 里的端口映射改成 `127.0.0.1:8080:8080`，并设置 `PLEXROUTER_TRUST_PROXY=true`。用 [Caddy](https://caddyserver.com)（自动申请证书）时，`/etc/caddy/Caddyfile`：

```
api.example.com {
	reverse_proxy 127.0.0.1:8080 {
		flush_interval -1
	}
}
```

用 nginx 时注意不要缓冲或过早断开流式响应：`proxy_buffering off;`、`proxy_read_timeout 600s;`、`client_max_body_size 50m;`。

**升级。** 启动时会自动执行数据库迁移：

```bash
docker compose pull && docker compose up -d
```

**备份。** 所有数据都在 `plexrouter_data` 卷里（数据库 + `secret.key`）：

```bash
docker compose stop
docker run --rm -v plexrouter_data:/data -v "$PWD":/backup alpine tar czf /backup/plexrouter-$(date +%F).tgz -C /data .
docker compose start
```

## 调用示例

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-plex-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o-mini",
    "messages": [{"role": "user", "content": "你好！"}]
  }'
```

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="sk-plex-...")
resp = client.chat.completions.create(
    model="deepseek/deepseek-chat",
    messages=[{"role": "user", "content": "用一句话介绍你自己"}],
)
print(resp.choices[0].message.content)
```

接口同时挂在 `/api/v1` 下，需要 OpenRouter 风格地址的工具也能直接使用。

| 接口 | 说明 |
|---|---|
| `POST /v1/chat/completions` | 对话补全（OpenAI 格式，支持 `stream: true`）。额外请求字段 `models: string[]` 为回退模型列表。响应中包含 `provider` 和 `usage.cost`（美元）。 |
| `POST /v1/embeddings` | 向量嵌入（OpenAI 及 OpenAI 兼容供应商）。 |
| `POST /v1/messages` | Anthropic Messages API（支持流式、工具、思考、缓存），以及 `/v1/messages/count_tokens`。用 `x-api-key` 或 `Authorization: Bearer` 认证。 |
| `GET /v1/models` | 已启用的模型，含上下文长度和每 token 价格。 |
| `GET /v1/key` | 当前 Key 的名称、已用额度、额度上限和账户余额。 |

### Claude Code

PlexRouter 提供 Anthropic Messages API，所以 Claude Code 可以使用模型目录里的任意模型：

```bash
export ANTHROPIC_BASE_URL=https://api.example.com   # 不要带 /v1，Claude Code 会自己加上 /v1/messages
export ANTHROPIC_AUTH_TOKEN=sk-plex-...
export ANTHROPIC_MODEL=claude-sonnet-4-5            # 可选；目录中任意模型 ID
claude
```

- 发往 Anthropic 供应商的请求**原样透传**（请求体逐字节保留、转发全部 `anthropic-*` 请求头、流式事件和错误体不做修改），提示词缓存、扩展思考、beta 功能和 Claude Code 的错误恢复都能正常工作。发往其他供应商的请求会自动与 OpenAI 格式互相转换。
- Claude Code 自己的模型名（如 `claude-sonnet-4-5-20250929`）会自动对应到目录中的模型。
- 使用 Claude Code 不认识的模型（如 `openai/gpt-4o`）时，把 `CLAUDE_CODE_MAX_CONTEXT_TOKENS` 设为该模型的上下文长度；使用 auto 模式时再设置 `CLAUDE_CODE_AUTO_MODE_SERVER=0`。
- 设置 `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1` 后，`/model` 里会列出网关上的 Claude 模型。

## 配置

全部通过环境变量配置（见 [`.env.example`](.env.example)），供应商和模型在控制台里管理。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PLEXROUTER_ADDR` | `:8080` | 监听地址 |
| `PLEXROUTER_DATA_DIR` | `./data` | SQLite 数据库和自动生成密钥的目录 |
| `PLEXROUTER_DB` | `$DATA_DIR/plexrouter.db` | 数据库文件 |
| `PLEXROUTER_SECRET` | 自动生成 | 用于加密供应商 Key。未设置时生成到 `$DATA_DIR/secret.key`，请妥善备份 |
| `PLEXROUTER_NAME` | `PlexRouter` | 控制台显示的名称 |
| `PLEXROUTER_ALLOW_REGISTER` | `true` | 是否为首次登录的 Google/GitHub 用户新建账号 |
| `PLEXROUTER_SIGNUP_CREDIT` | `0` | 新用户赠送的美元余额 |
| `PLEXROUTER_ADMIN_EMAILS` | | 管理员邮箱，逗号分隔（关闭注册时也能登录）。不设置时第一个登录的用户是管理员 |
| `PLEXROUTER_REQUEST_TIMEOUT` | `10m` | 单次上游请求的最长时间 |
| `PLEXROUTER_TRUST_PROXY` | `false` | 在反向代理后时，用 `X-Forwarded-For` 取客户端 IP |
| `PLEXROUTER_PUBLIC_URL` | 从请求推断 | 对外访问的地址，用于 OAuth 回调 |
| `PLEXROUTER_GOOGLE_CLIENT_ID` / `_SECRET` | | 启用 Google 登录 |
| `PLEXROUTER_GITHUB_CLIENT_ID` / `_SECRET` | | 启用 GitHub 登录 |
| `PLEXROUTER_LOG_LEVEL` | `info` | `debug`、`info`、`warn`、`error` |
| `PLEXROUTER_DEFAULT_USER_RPM` | `0` | 每个用户每分钟最多请求数，可在用户上单独设置（0 = 不限） |
| `PLEXROUTER_LOG_RETENTION_DAYS` | `90` | 请求日志保留天数（0 = 永久保留）。报表只统计保留期内的日志 |

### Google / GitHub 登录

登录只支持 Google 和/或 GitHub，至少要配置一个，否则没人能登录控制台。配置好对应的 Client ID 和 Secret 后，登录页就会出现相应的按钮。回调地址是 `<PLEXROUTER_PUBLIC_URL>/api/auth/oauth/<provider>/callback`，`PLEXROUTER_PUBLIC_URL` 填用户实际访问的地址，例如 `https://api.example.com`。不设置时从请求中推断。

- **Google**：打开 [Google Cloud Console → 凭据](https://console.cloud.google.com/apis/credentials)，选择「创建凭据 → OAuth 客户端 ID → Web 应用」，在已获授权的重定向 URI 中添加 `https://api.example.com/api/auth/oauth/google/callback`，然后填写 `PLEXROUTER_GOOGLE_CLIENT_ID` / `PLEXROUTER_GOOGLE_CLIENT_SECRET`。
- **GitHub**：打开 [Settings → Developer settings → OAuth Apps → New OAuth App](https://github.com/settings/developers)，Authorization callback URL 填 `https://api.example.com/api/auth/oauth/github/callback`，然后填写 `PLEXROUTER_GITHUB_CLIENT_ID` / `PLEXROUTER_GITHUB_CLIENT_SECRET`。

Google / GitHub 账号的已验证邮箱与现有 PlexRouter 账号相同时，会自动关联到该账号；否则新建账号（关闭注册时不会新建）。用户可以在「设置」里绑定或解绑第三方账号，但至少要保留一个。

## 计费规则

- 每个模型设置输入、输出价格（**美元 / 百万 token**）。费用 = `prompt_tokens × 输入价 + completion_tokens × 输出价`，token 数以上游返回为准（上游不返回时才估算）。
- 付费模型需要余额大于 0；价格为 0 的模型免费。Key 可以设置额度上限。
- 上游失败的尝试会记录日志但不扣费；流式输出中途中断时，按已输出的部分计费。
- 余额由管理员调整（用户 → 调整余额）或通过兑换码充值。在线支付在规划中。
- **利润**：在「模型管理」里为每条线路填写上游成本（供应商向你收的价格，美元 / 百万 token），或者用目录导入自动填写。之后「管理 → 报表」会按日期、模型、供应商展示收入、上游成本和利润。没填成本的线路，请求会计为"成本未知"。

## 运营说明

- **供应商 Key 池**：每个供应商可以添加多个 Key（管理 → 供应商 → Key），请求在这些 Key 之间轮流。返回 `401` 或"Key 无效"的会被标记为*认证失败*，提示没有额度的会被标记为*额度用尽*，这两种都会停用，直到你手动重新启用。返回 `429` 的 Key 会短暂冷却（遵循 `Retry-After`）。
- **故障切换**：一条线路连续失败 3 次（5xx、超时）后跳过 30 秒，之后翻倍，最长 5 分钟，期间由该模型的其他线路接管。每个供应商显示最近 24 小时的健康度。
- **告警**：管理概览会列出失效的 Key、没有可用 Key 或成功率过低的供应商、已启用但没有可用线路的模型，以及没填成本的付费模型。
- **目录导入**：管理 → 模型管理 → *从目录导入*，会拉取 OpenRouter 的公开模型列表，与某个供应商的模型自动匹配（例如 `anthropic/claude-sonnet-4.5` ↔ `claude-sonnet-4-5-20250929`），并按"参考价 ×（1 + 加价比例）"创建模型。服务器需要能访问 `openrouter.ai`。
- 熔断、冷却和限流状态保存在内存里，服务重启后会重置。

## 目录结构

```
cmd/plexrouter/     程序入口
internal/config/    环境变量配置
internal/store/     SQLite 存储与迁移
internal/provider/  上游适配器：OpenAI（兼容）、Anthropic、Gemini
internal/gateway/   /v1 接口：鉴权、路由、回退、流式、计费
internal/server/    控制台 JSON API、会话、内嵌前端
web/                React + Vite + Tailwind 控制台（编译后嵌入二进制）
docs/               控制台 API 约定
```

## 开发

```bash
make run        # 后端运行在 :8080
make dev-web    # 控制台运行在 :5173，热更新，请求代理到后端
make test       # Go 测试（使用模拟上游的端到端测试）
```

## 路线图

- 在线支付（Stripe、支付宝等）
- 更多供应商：Azure OpenAI、AWS Bedrock、Vertex AI
- 按权重负载均衡、按价格/延迟选路
- OpenRouter 风格的路由参数（`provider.order`、`sort`、`allow_fallbacks`）和单次请求用量查询接口

欢迎贡献代码，较大的改动请先开 issue 讨论。

## 许可证

[MIT](LICENSE)
