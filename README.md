# PlexRouter

**A self-hosted, OpenAI-compatible LLM gateway.** Put OpenAI, Anthropic, Google Gemini and any OpenAI-compatible API (DeepSeek, Groq, Mistral, Together, vLLM, Ollama…) behind one endpoint, hand out API keys, and bill usage per token from prepaid credits — all from a single Go binary with a built-in web console.

[简体中文](README.zh-CN.md)

> PlexRouter is an independent open-source project inspired by services like OpenRouter. It is not affiliated with OpenRouter, Inc.

## Features

- **One API for every model** — `POST /v1/chat/completions` in the OpenAI format (streaming, tools/function calling, images, JSON mode), `/v1/embeddings`, `/v1/models`. Works with the official OpenAI SDKs and anything that speaks the OpenAI API.
- **Native adapters** — requests are translated to the Anthropic Messages API and the Gemini API (including tool calls and streaming); OpenAI-compatible upstreams get a pass-through that preserves extra fields.
- **Routing and fallback** — each model (`openai/gpt-4o`) can be served by several providers, tried in priority order. Clients can also pass `models: [...]` to fall back to other models. Fallback happens transparently before the first byte is sent.
- **Credits and billing** — prices per 1M input/output tokens per model; every request is charged exactly (nano-dollar precision) from the account balance. Per-key credit limits, redeem codes, admin adjustments, signup bonus.
- **Sign in with Google / GitHub** — optional OAuth login, with automatic account linking by verified email.
- **Web console** — users manage API keys, see usage, logs and spend charts, browse the model catalog and docs; admins manage providers (with connection test and one-click model import), models and endpoints, users, request logs and redeem codes. 10 languages (English, 简体中文, 繁體中文, 日本語, 한국어, Español, Français, Deutsch, Português, Русский), light and dark.
- **Easy to run** — one static binary, embedded SQLite (pure Go, no CGO), zero required config. Docker image and docker-compose included.
- **Secure defaults** — provider API keys are encrypted at rest (AES-256-GCM), only hashes of user API keys are stored, bcrypt passwords, HttpOnly session cookies, CSRF protection, login rate limiting.

## Quick start

### Docker

```bash
docker run -d --name plexrouter -p 8080:8080 -v plexrouter_data:/data ghcr.io/clouly/plexrouter:latest
```

or with compose:

```bash
docker compose up -d
```

### Binary

Download a binary from [Releases](https://github.com/clouly/plexrouter/releases) and run it:

```bash
./plexrouter
```

### From source

Requires Go 1.23+ and Node.js 20+.

```bash
git clone https://github.com/clouly/plexrouter.git
cd plexrouter
make build        # builds the console into web/dist and the server into bin/
./bin/plexrouter
```

Then open <http://localhost:8080>:

1. **Register** — the first account becomes the admin.
2. **Admin → Providers** — add a provider (e.g. type `openai` with your OpenAI key, or `openai_compatible` with `https://api.deepseek.com/v1`). Click **Test**, then **Import models**.
3. **Admin → Models** — imported models start *disabled with price 0*. Set prices (USD per 1M tokens) and enable them. A model can have several endpoints (providers) for fallback.
4. **Admin → Users** — give yourself some credits (or create redeem codes).
5. **API Keys** — create a key and call the API.

## Production deployment (single server with Docker)

Any Linux server with Docker works; one instance with SQLite comfortably handles a team or a small public service.

```bash
git clone https://github.com/clouly/plexrouter.git && cd plexrouter
cp .env.example .env
```

Edit `.env`:

- `PLEXROUTER_SECRET` — set it to the output of `openssl rand -hex 32` and keep a copy. It encrypts provider API keys; losing or changing it makes them unreadable.
- `PLEXROUTER_ADMIN_EMAIL` / `PLEXROUTER_ADMIN_PASSWORD` — creates the admin on first start.
- `PLEXROUTER_ALLOW_REGISTER=false` — if only invited users should have accounts.
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
| `GET /v1/models` | Enabled models with context length and per-token pricing. |
| `GET /v1/key` | Name, usage, limit and account balance of the calling key. |

## Configuration

Everything is configured with environment variables (see [`.env.example`](.env.example)); providers and models are managed in the console.

| Variable | Default | Description |
|---|---|---|
| `PLEXROUTER_ADDR` | `:8080` | Listen address |
| `PLEXROUTER_DATA_DIR` | `./data` | Directory for the SQLite database and generated secret |
| `PLEXROUTER_DB` | `$DATA_DIR/plexrouter.db` | Database file |
| `PLEXROUTER_SECRET` | generated | Encrypts provider keys at rest. Auto-generated into `$DATA_DIR/secret.key` if unset — back it up |
| `PLEXROUTER_NAME` | `PlexRouter` | Name shown in the console |
| `PLEXROUTER_ALLOW_REGISTER` | `true` | Allow public sign-up |
| `PLEXROUTER_SIGNUP_CREDIT` | `0` | USD credited to new accounts |
| `PLEXROUTER_ADMIN_EMAIL` / `PLEXROUTER_ADMIN_PASSWORD` | | Create this admin on first start |
| `PLEXROUTER_REQUEST_TIMEOUT` | `10m` | Upper bound for one upstream request |
| `PLEXROUTER_TRUST_PROXY` | `false` | Use `X-Forwarded-For` for client IPs (behind a reverse proxy) |
| `PLEXROUTER_PUBLIC_URL` | from request | Public base URL, used for OAuth callbacks |
| `PLEXROUTER_GOOGLE_CLIENT_ID` / `_SECRET` | | Enable “Sign in with Google” |
| `PLEXROUTER_GITHUB_CLIENT_ID` / `_SECRET` | | Enable “Sign in with GitHub” |
| `PLEXROUTER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

### Sign in with Google / GitHub

Optional. Each button appears on the login page once its client ID and secret are set. The callback URL is `<PLEXROUTER_PUBLIC_URL>/api/auth/oauth/<provider>/callback`. Set `PLEXROUTER_PUBLIC_URL` to the address users open, e.g. `https://api.example.com`. If it is unset, the address is taken from the request.

- **Google**: [Google Cloud Console → Credentials](https://console.cloud.google.com/apis/credentials) → *Create credentials → OAuth client ID → Web application*. Add `https://api.example.com/api/auth/oauth/google/callback` as an authorized redirect URI, then set `PLEXROUTER_GOOGLE_CLIENT_ID` / `PLEXROUTER_GOOGLE_CLIENT_SECRET`.
- **GitHub**: [Settings → Developer settings → OAuth Apps → New OAuth App](https://github.com/settings/developers). Set the authorization callback URL to `https://api.example.com/api/auth/oauth/github/callback`, then set `PLEXROUTER_GITHUB_CLIENT_ID` / `PLEXROUTER_GITHUB_CLIENT_SECRET`.

A Google or GitHub account is linked to an existing PlexRouter account automatically when the verified email addresses match. Otherwise a new account is created, unless registration is disabled. Users can connect or disconnect providers, and set a password, under **Settings**.

## How billing works

- Each model has an input and output price in **USD per 1M tokens**. Cost = `prompt_tokens × input_price + completion_tokens × output_price`, using the token counts reported by the upstream (estimated only if the upstream reports none).
- Paid models require a positive balance; models priced at 0 are free. Keys can carry a credit limit.
- Failed upstream attempts are logged but not charged. A streamed response that breaks midway is charged for what was delivered.
- Credits are added by admins (Users → Adjust credits) or by redeem codes. Payment-provider integration is on the roadmap.

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

- Native Anthropic `/v1/messages` endpoint (for Claude Code and Anthropic SDKs)
- Online payments (Stripe, etc.)
- Per-key rate limits and model allow-lists
- More providers: Azure OpenAI, AWS Bedrock, Vertex AI
- Load balancing by weight and health-based routing

Contributions are welcome — please open an issue to discuss larger changes first.

## License

[MIT](LICENSE)
