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

Users sign in with **GitHub or Google** (there are no passwords), so first create an OAuth app. GitHub is the quickest: [Settings → Developer settings → OAuth Apps → New OAuth App](https://github.com/settings/developers), with homepage `http://localhost:8080` and callback URL `http://localhost:8080/api/auth/oauth/github/callback`. Copy the client ID and generate a client secret.

### Docker

```bash
docker run -d --name plexrouter -p 8080:8080 -v plexrouter_data:/data \
  -e PLEXROUTER_GITHUB_CLIENT_ID=<client-id> -e PLEXROUTER_GITHUB_CLIENT_SECRET=<client-secret> \
  ghcr.io/local4a/plexrouter:latest
```

or with compose (put the two variables in `.env`):

```bash
docker compose up -d
```

### Binary

Download a binary from [Releases](https://github.com/local4a/plexrouter/releases) and run it:

```bash
./plexrouter
```

### From source

Requires Go 1.23+ and Node.js 20+.

```bash
git clone https://github.com/local4a/plexrouter.git
cd plexrouter
make build        # builds the console into web/dist and the server into bin/
./bin/plexrouter
```

Then open <http://localhost:8080>:

1. **Sign in with GitHub** — the first account becomes the admin (or list admins in `PLEXROUTER_ADMIN_EMAILS`).
2. **Admin → Providers** — add a provider (e.g. type `openai` with your OpenAI key, or `openai_compatible` with `https://api.deepseek.com/v1`). Click **Test**, then **Import models**.
3. **Admin → Models** — imported models start *disabled with price 0*. Set prices (USD per 1M tokens) and enable them. A model can have several endpoints (providers) for fallback.
4. **Admin → Users** — give yourself some credits (or create redeem codes).
5. **API Keys** — create a key and call the API.

## Production deployment (single server with Docker)

Any Linux server with Docker works; one instance with SQLite comfortably handles a team or a small public service.

```bash
git clone https://github.com/local4a/plexrouter.git && cd plexrouter
cp .env.example .env
```

Edit `.env`:

- `PLEXROUTER_SECRET` — set it to the output of `openssl rand -hex 32` and keep a copy. It encrypts provider API keys; losing or changing it makes them unreadable.
- `PLEXROUTER_GITHUB_CLIENT_ID` / `_SECRET` and/or `PLEXROUTER_GOOGLE_CLIENT_ID` / `_SECRET`, plus `PLEXROUTER_PUBLIC_URL` (e.g. `https://api.example.com`) — sign-in; see [Sign in with Google / GitHub](#sign-in-with-google--github).
- `PLEXROUTER_ADMIN_EMAILS` — your email, so you become admin no matter who signs in first.
- `PLEXROUTER_ALLOW_REGISTER=false` — if only existing accounts (and listed admins) may sign in.
- `HTTPS_PROXY` — if the server can't reach the model APIs directly.

```bash
docker compose up -d          # or: docker compose up -d --build  (build from source)
docker compose logs -f
```

**HTTPS.** Put a reverse proxy in front, change the port mapping in `docker-compose.yml` to `127.0.0.1:8080:8080`, and set `PLEXROUTER_TRUST_PROXY=true`. With [Caddy](https://caddyserver.com) (automatic certificates), `/etc/caddy/Caddyfile`:

```
api.example.com {
	reverse_proxy 127.0.0.1:8080 {
		flush_interval -1
	}
}
```

With nginx, make sure streaming isn't buffered or cut off: `proxy_buffering off;`, `proxy_read_timeout 600s;`, `client_max_body_size 50m;`.

**Upgrade.** Migrations run automatically on start:

```bash
docker compose pull && docker compose up -d
```

**Backup.** Everything lives in the `plexrouter_data` volume (database + `secret.key`):

```bash
docker compose stop
docker run --rm -v plexrouter_data:/data -v "$PWD":/backup alpine tar czf /backup/plexrouter-$(date +%F).tgz -C /data .
docker compose start
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

Everything is configured with environment variables (see [`.env.example`](.env.example)); providers and models are managed in the console.

| Variable | Default | Description |
|---|---|---|
| `PLEXROUTER_ADDR` | `:8080` | Listen address |
| `PLEXROUTER_DATA_DIR` | `./data` | Directory for the SQLite database and generated secret |
| `PLEXROUTER_DB` | `$DATA_DIR/plexrouter.db` | Database file |
| `PLEXROUTER_SECRET` | generated | Encrypts provider keys at rest. Auto-generated into `$DATA_DIR/secret.key` if unset — back it up |
| `PLEXROUTER_NAME` | `PlexRouter` | Name shown in the console |
| `PLEXROUTER_ALLOW_REGISTER` | `true` | Create accounts for new Google/GitHub users |
| `PLEXROUTER_SIGNUP_CREDIT` | `0` | USD credited to new accounts |
| `PLEXROUTER_ADMIN_EMAILS` | | Comma-separated emails that are always admins (can sign up even when registration is closed). If unset, the first user is admin |
| `PLEXROUTER_REQUEST_TIMEOUT` | `10m` | Upper bound for one upstream request |
| `PLEXROUTER_TRUST_PROXY` | `false` | Use `X-Forwarded-For` for client IPs (behind a reverse proxy) |
| `PLEXROUTER_PUBLIC_URL` | from request | Public base URL, used for OAuth callbacks |
| `PLEXROUTER_GOOGLE_CLIENT_ID` / `_SECRET` | | Enable “Sign in with Google” |
| `PLEXROUTER_GITHUB_CLIENT_ID` / `_SECRET` | | Enable “Sign in with GitHub” |
| `PLEXROUTER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PLEXROUTER_DEFAULT_USER_RPM` | `0` | Requests per minute per user unless set on the user (0 = unlimited) |
| `PLEXROUTER_LOG_RETENTION_DAYS` | `90` | Delete request logs older than this (0 = keep forever). Reports only cover kept logs |

### Sign in with Google / GitHub

Sign-in is only through Google and/or GitHub — configure at least one, or nobody can log in to the console. Each button appears on the login page once its client ID and secret are set. The callback URL is `<PLEXROUTER_PUBLIC_URL>/api/auth/oauth/<provider>/callback`. Set `PLEXROUTER_PUBLIC_URL` to the address users open, e.g. `https://api.example.com`. If it is unset, the address is taken from the request.

- **Google**: [Google Cloud Console → Credentials](https://console.cloud.google.com/apis/credentials) → *Create credentials → OAuth client ID → Web application*. Add `https://api.example.com/api/auth/oauth/google/callback` as an authorized redirect URI, then set `PLEXROUTER_GOOGLE_CLIENT_ID` / `PLEXROUTER_GOOGLE_CLIENT_SECRET`.
- **GitHub**: [Settings → Developer settings → OAuth Apps → New OAuth App](https://github.com/settings/developers). Set the authorization callback URL to `https://api.example.com/api/auth/oauth/github/callback`, then set `PLEXROUTER_GITHUB_CLIENT_ID` / `PLEXROUTER_GITHUB_CLIENT_SECRET`.

A Google or GitHub account is linked to an existing PlexRouter account automatically when the verified email addresses match. Otherwise a new account is created, unless registration is disabled. Under **Settings**, users can connect or disconnect providers; they must keep at least one connected.

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

Contributions are welcome — please open an issue to discuss larger changes first.

## License

[MIT](LICENSE)
