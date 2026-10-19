# PlexRouter

**A self-hosted, OpenAI-compatible LLM gateway.** Put OpenAI, Anthropic, Google Gemini and any OpenAI-compatible API (DeepSeek, Groq, Mistral, Together, vLLM, Ollama…) behind one endpoint, hand out API keys, and bill usage per token from prepaid credits — all from a single Go binary with a built-in web console.

[简体中文](README.zh-CN.md)

> PlexRouter is an independent open-source project inspired by services like OpenRouter. It is not affiliated with OpenRouter, Inc.

## Features

- **One API for every model** — `POST /v1/chat/completions` in the OpenAI format (streaming, tools/function calling, images, JSON mode), `/v1/embeddings`, `/v1/models`. Works with the official OpenAI SDKs and anything that speaks the OpenAI API.
- **Anthropic Messages API and Claude Code** — `POST /v1/messages` passes through unchanged to Anthropic and is translated for every other provider, so Claude Code and Anthropic SDKs can use any model.
- **Native adapters** — requests are translated to the Anthropic Messages API and the Gemini API (including tool calls and streaming); OpenAI-compatible upstreams get a pass-through that preserves extra fields. Reasoning effort, prompt caching (`cache_control`) and structured outputs are mapped across providers, and cached tokens are billed at cache prices.
- **Routing and fallback** — each model (`openai/gpt-4o`) can be served by several providers, tried in priority order. Clients can also pass `models: [...]` to fall back to other models. Fallback happens transparently before the first byte is sent.
- **Credits and billing** — prices per 1M input/output tokens per model; every request is charged exactly (nano-dollar precision) from the account balance. Per-key credit limits, redeem codes, admin adjustments, signup bonus.
- **Built for operators** — your users never touch provider keys. Pools of upstream keys per provider with rotation; keys that are rejected or run out of credit are switched off automatically, rate-limited ones cool down, and failing endpoints are skipped by a circuit breaker. Track upstream cost next to revenue for profit reports, see provider health and alerts, and import models with reference prices from the OpenRouter catalog with your own markup.
- **Limits** — requests-per-minute limits per user (with a server default) and per key; keys can be restricted to a list of models.
- **Sign in with Google / GitHub** — no passwords to manage; accounts are created on first sign-in and linked by verified email.
- **Web console** — users manage API keys, see usage, logs and spend charts, browse the model catalog and docs; admins manage providers (with connection test and one-click model import), models and endpoints, users, request logs and redeem codes. 10 languages (English, 简体中文, 繁體中文, 日本語, 한국어, Español, Français, Deutsch, Português, Русский), light and dark.
- **Easy to run** — one static binary, embedded SQLite (pure Go, no CGO), zero required config. Docker image and docker-compose included.
- **Secure defaults** — provider API keys are encrypted at rest (AES-256-GCM), only hashes of user API keys are stored, no passwords (sign-in via Google/GitHub with PKCE), HttpOnly session cookies, CSRF protection, rate limiting.

## Quick start

```bash
docker run -d --name plexrouter -p 8080:8080 -v plexrouter_data:/data ghcr.io/local4a/plexrouter:latest
```

Open <http://localhost:8080> (or your server's address). A **setup wizard** connects GitHub or Google sign-in: it shows the exact homepage and callback URLs to paste when you create the OAuth app, so it takes a couple of minutes. The first person to sign in becomes the admin, and everything else is configured in the console:

1. **Admin → Providers**: add your OpenAI, Anthropic, Gemini, DeepSeek… API keys, then click **Test**.
2. **Admin → Models**: **Import from catalog** (names, context lengths and prices included, with your markup), or add models by hand.
3. **Admin → Users**: give credits, or create redeem codes.
4. **API Keys**: create a key and call the API.

> Until someone has signed in as admin, whoever opens the page can run the setup. Finish it right after starting the container.

Other ways to run it:

- **docker compose**: `docker compose up -d` with this repository's [`docker-compose.yml`](docker-compose.yml).
- **Binary**: download one from [Releases](https://github.com/local4a/plexrouter/releases) and run `./plexrouter`.
- **From source** (Go 1.23+, Node.js 20+): `git clone https://github.com/local4a/plexrouter.git && cd plexrouter && make build && ./bin/plexrouter`.

## Production deployment (single server with Docker)

Any Linux server with Docker works; one instance with SQLite comfortably handles a team or a small public service. Start it as in [Quick start](#quick-start), run the setup wizard, then:

- **HTTPS**: put a reverse proxy in front, publish the container on `127.0.0.1:8080:8080` only, and in **Admin → System settings** set the *public URL* (e.g. `https://api.example.com`) and turn on *trust proxy*. With [Caddy](https://caddyserver.com) (automatic certificates), `/etc/caddy/Caddyfile`:

  ```
  api.example.com {
  	reverse_proxy 127.0.0.1:8080 {
  		flush_interval -1
  	}
  }
  ```

  With nginx, make sure streaming isn't buffered or cut off: `proxy_buffering off;`, `proxy_read_timeout 600s;`, `client_max_body_size 50m;`. If you set up sign-in on `http://localhost` first, update the callback URL in your GitHub/Google OAuth app to the HTTPS address.
- **No direct internet access to the model APIs** (e.g. servers in mainland China): pass an outbound proxy with `-e HTTPS_PROXY=http://proxy:port`, or use domestic providers (DeepSeek, Qwen, Doubao…, type `openai_compatible`). If pulling from `ghcr.io` is slow, build on the server: `git clone … && docker compose up -d --build`.
- **Upgrade**: `docker pull ghcr.io/local4a/plexrouter:latest`, then recreate the container (`docker compose pull && docker compose up -d`). Migrations run automatically; read [CHANGELOG.md](CHANGELOG.md) first.
- **Backup**: everything lives in the `plexrouter_data` volume, including the database and `secret.key`, which encrypts provider keys:

  ```bash
  docker stop plexrouter
  docker run --rm -v plexrouter_data:/data -v "$PWD":/backup alpine tar czf /backup/plexrouter-$(date +%F).tgz -C /data .
  docker start plexrouter
  ```

## Using the API

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-plex-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o-mini",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="sk-plex-...")
stream = client.chat.completions.create(
    model="anthropic/claude-sonnet-4-5",
    messages=[{"role": "user", "content": "Write a haiku about routers"}],
    stream=True,
)
for chunk in stream:
    if chunk.choices:
        print(chunk.choices[0].delta.content or "", end="")
```

The base URL is also served at `/api/v1`, so tools that expect an OpenRouter-style URL work too.

| Endpoint | Description |
|---|---|
| `POST /v1/chat/completions` | Chat completions (OpenAI format, `stream: true` supported). Extra request field `models: string[]` lists fallback models. The response includes `provider` and `usage.cost` (USD). |
| `POST /v1/embeddings` | Embeddings (OpenAI and OpenAI-compatible providers). |
| `POST /v1/messages` | Anthropic Messages API (streaming, tools, thinking, caching), plus `/v1/messages/count_tokens`. Auth with `x-api-key` or `Authorization: Bearer`. |
| `GET /v1/models` | Enabled models with context length and per-token pricing. |
| `GET /v1/key` | Name, usage, limit and account balance of the calling key. |

### Claude Code

PlexRouter serves the Anthropic Messages API, so Claude Code works with any model in your catalog:

```bash
export ANTHROPIC_BASE_URL=https://api.example.com   # no /v1 — Claude Code appends /v1/messages
export ANTHROPIC_AUTH_TOKEN=sk-plex-...
export ANTHROPIC_MODEL=claude-sonnet-4-5            # optional; any model ID from the catalog
claude
```

- Requests to Anthropic providers pass through **unchanged** (body byte-for-byte, all `anthropic-*` headers, streaming events, error bodies), so prompt caching, extended thinking, beta features and Claude Code's error recovery keep working. Other providers are translated to and from the OpenAI format.
- Claude Code's own model names (`claude-sonnet-4-5-20250929`) resolve to the matching catalog model.
- For a model Claude Code doesn't know (e.g. `openai/gpt-4o`), set `CLAUDE_CODE_MAX_CONTEXT_TOKENS` to its context window, and in auto mode `CLAUDE_CODE_AUTO_MODE_SERVER=0`.
- `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1` lists the gateway's Claude models in `/model`.

## Configuration

Everything is configured in the console, under **Admin → System settings**: site name, public URL, GitHub/Google sign-in, registration, signup credit, default rate limit, request timeout, log retention and proxy trust. Providers, models, users and prices have their own admin pages.

Environment variables are **optional**. They suit automated deployments, and a value saved in the console overrides them:

| Variable | Default | Description |
|---|---|---|
| `PLEXROUTER_ADDR` | `:8080` | Listen address (also `PORT`) |
| `PLEXROUTER_DATA_DIR` | `./data` (`/data` in Docker) | Directory for the SQLite database and generated secret |
| `PLEXROUTER_SECRET` | generated | Encrypts provider keys at rest; generated into the data directory if unset. Back it up |
| `PLEXROUTER_ADMIN_EMAILS` | | Comma-separated emails that always become admins when they sign in |
| `PLEXROUTER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `HTTPS_PROXY` / `NO_PROXY` | | Outbound proxy for upstream model APIs |
| `PLEXROUTER_NAME`, `PLEXROUTER_PUBLIC_URL`, `PLEXROUTER_ALLOW_REGISTER`, `PLEXROUTER_SIGNUP_CREDIT`, `PLEXROUTER_DEFAULT_USER_RPM`, `PLEXROUTER_REQUEST_TIMEOUT`, `PLEXROUTER_LOG_RETENTION_DAYS`, `PLEXROUTER_TRUST_PROXY`, `PLEXROUTER_GITHUB_CLIENT_ID`/`_SECRET`, `PLEXROUTER_GOOGLE_CLIENT_ID`/`_SECRET` | | Initial values for the console settings of the same name |

### Sign in with Google / GitHub

Users sign in only with Google and/or GitHub; there are no passwords. The setup wizard and **Admin → System settings** show the callback URL to register for each provider (`<public URL>/api/auth/oauth/<provider>/callback`):

- **GitHub**: [New OAuth App](https://github.com/settings/applications/new): homepage = your public URL, authorization callback URL = the one shown.
- **Google**: [Google Cloud Console → Credentials](https://console.cloud.google.com/apis/credentials) → *Create credentials → OAuth client ID → Web application*, with the shown URL as authorized redirect URI.

A Google or GitHub account is linked to an existing PlexRouter account automatically when the verified email addresses match. Otherwise a new account is created, unless registration is disabled. Users can connect or disconnect providers under **Settings**; at least one must stay connected.

## How billing works

- Each model has an input and output price in **USD per 1M tokens**. Cost = `prompt_tokens × input_price + completion_tokens × output_price`, using the token counts reported by the upstream (estimated only if the upstream reports none).
- Paid models require a positive balance; models priced at 0 are free. Keys can carry a credit limit.
- Failed upstream attempts are logged but not charged. A streamed response that breaks midway is charged for what was delivered.
- Credits are added by admins (Users → Adjust credits) or by redeem codes. Payment-provider integration is on the roadmap.
- **Profit**: set each endpoint's upstream cost (what the provider charges you, USD per 1M tokens) on the Models page, or import models from the catalog, which fills it in. **Admin → Reports** then shows revenue, upstream cost and profit by day, model and provider. Requests through endpoints without a cost count as "unknown cost".

## Running a service

- **Provider keys**: add several keys per provider (Admin → Providers → Keys); requests rotate across them. A key that gets `401`/an invalid-key error is marked *auth failed*, and one that reports no credit/quota is marked *quota exceeded*. Both stop being used until you re-enable them. A `429` puts a key on a short cooldown (honouring `Retry-After`).
- **Failover**: when an endpoint fails 3 times in a row (5xx, timeouts), it is skipped for 30 s, doubling up to 5 min, while other endpoints of the model take over. Health over the last 24 h is shown per provider.
- **Alerts**: the admin overview lists failed keys, providers without a usable key or with a low success rate, enabled models nobody can serve, and paid models without a cost.
- **Catalog import**: Admin → Models → *Import from catalog* loads OpenRouter's public model list, matches it against a provider's models (e.g. `anthropic/claude-sonnet-4.5` ↔ `claude-sonnet-4-5-20250929`), and creates models priced at reference × (1 + markup). The server needs outbound access to `openrouter.ai`.
- Circuit breakers, cooldowns and rate limits are kept in memory, so they reset when the server restarts.

## Project layout

```
cmd/plexrouter/     entry point
internal/config/    environment configuration
internal/store/     SQLite persistence and migrations
internal/provider/  upstream adapters: OpenAI(-compatible), Anthropic, Gemini
internal/gateway/   /v1 API: auth, routing, fallback, streaming, billing
internal/server/    console JSON API, sessions, embedded SPA
web/                React + Vite + Tailwind console (embedded into the binary)
docs/               console API contract
```

## Development

```bash
make run        # backend on :8080
make dev-web    # console on :5173 with hot reload, proxied to the backend
make test       # Go tests (end-to-end against fake upstreams)
```

## Roadmap

- Online payments (Stripe, etc.)
- More providers: Azure OpenAI, AWS Bedrock, Vertex AI
- Weighted load balancing and price/latency-based routing
- OpenRouter-style routing parameters (`provider.order`, `sort`, `allow_fallbacks`) and a generation-stats endpoint

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md), and please open an issue to discuss larger changes first. Report security problems privately as described in [SECURITY.md](SECURITY.md). What changed in each version, including upgrade notes, is in [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE)
