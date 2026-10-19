# PlexRouter

**可自托管的 OpenAI 兼容大模型网关。** 把 OpenAI、Anthropic、Google Gemini 以及任何 OpenAI 兼容接口（DeepSeek、Groq、Mistral、Together、vLLM、Ollama……）统一到一个接口后面，发放 API Key，按 token 从预充值余额中计费——只需一个 Go 二进制文件，自带 Web 控制台。

[English](README.md)

> PlexRouter 是受 OpenRouter 等服务启发的独立开源项目，与 OpenRouter, Inc. 没有任何关联。

## 功能

- **一个接口调用所有模型** —— OpenAI 格式的 `POST /v1/chat/completions`（支持流式、工具调用、图片、JSON 模式）、`/v1/embeddings`、`/v1/models`。可直接使用官方 OpenAI SDK 及所有兼容 OpenAI 接口的工具。
- **原生协议适配** —— 自动转换为 Anthropic Messages API 和 Gemini API（含工具调用与流式）；OpenAI 兼容上游直接透传，保留额外字段。
- **路由与失败回退** —— 一个模型（如 `openai/gpt-4o`）可以由多个供应商提供，按优先级依次尝试；客户端也可以传 `models: [...]` 回退到其他模型。回退在向客户端发送第一个字节之前完成，对客户端透明。
- **余额与计费** —— 每个模型按每百万输入/输出 token 定价，每次请求按上游返回的 token 数精确扣费（纳美元精度）。支持 Key 额度上限、兑换码、管理员调账、注册赠送。
- **Google / GitHub 登录** —— 可选的 OAuth 登录，按已验证邮箱自动关联已有账号。
- **Web 控制台** —— 用户可管理 API Key、查看用量、日志和消费图表、浏览模型列表和文档；管理员可管理供应商（支持连通性测试和一键导入模型）、模型与路由、用户、请求日志和兑换码。支持 10 种语言（English、简体中文、繁體中文、日本語、한국어、Español、Français、Deutsch、Português、Русский）及浅色/深色主题。
- **部署简单** —— 单个静态二进制，内置 SQLite（纯 Go，无需 CGO），零必填配置。提供 Docker 镜像和 docker-compose。
- **默认安全** —— 供应商 API Key 加密存储（AES-256-GCM），用户 API Key 只存哈希，bcrypt 密码，HttpOnly 会话 Cookie，CSRF 防护，登录限流。

## 快速开始

### Docker

```bash
docker run -d --name plexrouter -p 8080:8080 -v plexrouter_data:/data ghcr.io/clouly/plexrouter:latest
```

或使用 compose：

```bash
docker compose up -d
```

### 二进制

从 [Releases](https://github.com/clouly/plexrouter/releases) 下载对应平台的文件后直接运行：

```bash
./plexrouter
```

### 从源码构建

需要 Go 1.23+ 和 Node.js 20+。

```bash
git clone https://github.com/clouly/plexrouter.git
cd plexrouter
make build        # 构建控制台到 web/dist，构建服务端到 bin/
./bin/plexrouter
```

然后打开 <http://localhost:8080>：

1. **注册** —— 第一个注册的账号自动成为管理员。
2. **管理 → 供应商** —— 添加供应商（例如类型选 `openai` 填 OpenAI Key，或选 `openai_compatible` 填 `https://api.deepseek.com/v1`）。点 **测试**，再点 **导入模型**。
3. **管理 → 模型管理** —— 导入的模型默认 *禁用且价格为 0*，设置价格（美元 / 百万 token）后启用。一个模型可以配置多个供应商实现回退。
4. **管理 → 用户** —— 给自己加点余额（或生成兑换码）。
5. **API Keys** —— 创建 Key 并调用接口。

## 生产部署（单台服务器 + Docker）

任何装了 Docker 的 Linux 服务器都可以。单实例 + SQLite 足够支撑一个团队或小规模的对外服务。

```bash
git clone https://github.com/clouly/plexrouter.git && cd plexrouter
cp .env.example .env
```

编辑 `.env`：

- `PLEXROUTER_SECRET` —— 填 `openssl rand -hex 32` 的输出并妥善保存。它用来加密供应商 API Key，丢失或修改后已保存的 Key 将无法解密。
- `PLEXROUTER_ADMIN_EMAIL` / `PLEXROUTER_ADMIN_PASSWORD` —— 首次启动时创建管理员。
- `PLEXROUTER_ALLOW_REGISTER=false` —— 不想开放注册时设置。
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
| `GET /v1/models` | 已启用的模型，含上下文长度和每 token 价格。 |
| `GET /v1/key` | 当前 Key 的名称、已用额度、额度上限和账户余额。 |

## 配置

全部通过环境变量配置（见 [`.env.example`](.env.example)），供应商和模型在控制台里管理。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PLEXROUTER_ADDR` | `:8080` | 监听地址 |
| `PLEXROUTER_DATA_DIR` | `./data` | SQLite 数据库和自动生成密钥的目录 |
| `PLEXROUTER_DB` | `$DATA_DIR/plexrouter.db` | 数据库文件 |
| `PLEXROUTER_SECRET` | 自动生成 | 用于加密供应商 Key。未设置时生成到 `$DATA_DIR/secret.key`，请妥善备份 |
| `PLEXROUTER_NAME` | `PlexRouter` | 控制台显示的名称 |
| `PLEXROUTER_ALLOW_REGISTER` | `true` | 是否开放注册 |
| `PLEXROUTER_SIGNUP_CREDIT` | `0` | 新用户赠送的美元余额 |
| `PLEXROUTER_ADMIN_EMAIL` / `PLEXROUTER_ADMIN_PASSWORD` | | 首次启动时创建的管理员 |
| `PLEXROUTER_REQUEST_TIMEOUT` | `10m` | 单次上游请求的最长时间 |
| `PLEXROUTER_TRUST_PROXY` | `false` | 在反向代理后时，用 `X-Forwarded-For` 取客户端 IP |
| `PLEXROUTER_PUBLIC_URL` | 从请求推断 | 对外访问的地址，用于 OAuth 回调 |
| `PLEXROUTER_GOOGLE_CLIENT_ID` / `_SECRET` | | 启用 Google 登录 |
| `PLEXROUTER_GITHUB_CLIENT_ID` / `_SECRET` | | 启用 GitHub 登录 |
| `PLEXROUTER_LOG_LEVEL` | `info` | `debug`、`info`、`warn`、`error` |

### Google / GitHub 登录

可选。配置好对应的 Client ID 和 Secret 后，登录页就会出现相应的按钮。回调地址是 `<PLEXROUTER_PUBLIC_URL>/api/auth/oauth/<provider>/callback`，`PLEXROUTER_PUBLIC_URL` 填用户实际访问的地址，例如 `https://api.example.com`。不设置时从请求中推断。

- **Google**：打开 [Google Cloud Console → 凭据](https://console.cloud.google.com/apis/credentials)，选择「创建凭据 → OAuth 客户端 ID → Web 应用」，在已获授权的重定向 URI 中添加 `https://api.example.com/api/auth/oauth/google/callback`，然后填写 `PLEXROUTER_GOOGLE_CLIENT_ID` / `PLEXROUTER_GOOGLE_CLIENT_SECRET`。
- **GitHub**：打开 [Settings → Developer settings → OAuth Apps → New OAuth App](https://github.com/settings/developers)，Authorization callback URL 填 `https://api.example.com/api/auth/oauth/github/callback`，然后填写 `PLEXROUTER_GITHUB_CLIENT_ID` / `PLEXROUTER_GITHUB_CLIENT_SECRET`。

Google / GitHub 账号的已验证邮箱与现有 PlexRouter 账号相同时，会自动关联到该账号；否则新建账号（关闭注册时不会新建）。用户可以在「设置」里绑定或解绑第三方账号，也可以设置密码。

## 计费规则

- 每个模型设置输入、输出价格（**美元 / 百万 token**）。费用 = `prompt_tokens × 输入价 + completion_tokens × 输出价`，token 数以上游返回为准（上游不返回时才估算）。
- 付费模型需要余额大于 0；价格为 0 的模型免费。Key 可以设置额度上限。
- 上游失败的尝试会记录日志但不扣费；流式输出中途中断时，按已输出的部分计费。
- 余额由管理员调整（用户 → 调整余额）或通过兑换码充值。在线支付在规划中。

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

- 原生 Anthropic `/v1/messages` 接口（支持 Claude Code 和 Anthropic SDK）
- 在线支付（Stripe、支付宝等）
- 按 Key 限流、模型白名单
- 更多供应商：Azure OpenAI、AWS Bedrock、Vertex AI
- 按权重负载均衡、基于健康度的路由

欢迎贡献代码，较大的改动请先开 issue 讨论。

## 许可证

[MIT](LICENSE)
