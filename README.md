# PlexRouter

**A self-hosted, OpenAI-compatible LLM gateway.** Put OpenAI, Anthropic, Google Gemini and any OpenAI-compatible API (DeepSeek, Groq, Mistral, Together, vLLM, Ollama…) behind one endpoint, hand out API keys, and bill usage per token from prepaid credits — all from a single Go binary with a built-in web console.

[简体中文](README.zh-CN.md)

> PlexRouter is an independent open-source project inspired by services like OpenRouter. It is not affiliated with OpenRouter, Inc.

## Features

- **One API for every model** — `POST /v1/chat/completions` in the OpenAI format (streaming, tools/function calling, images, JSON mode), `/v1/embeddings`, `/v1/models`. Works with the official OpenAI SDKs and anything that speaks the OpenAI API.
- **Native adapters** — requests are translated to the Anthropic Messages API and the Gemini API (including tool calls and streaming); OpenAI-compatible upstreams get a pass-through that preserves extra fields.
- **Routing and fallback** — each model (`openai/gpt-4o`) can be served by several providers, tried in priority order. Clients can also pass `models: [...]` to fall back to other models. Fallback happens transparently before the first byte is sent.
- **Credits and billing** — prices per 1M input/output tokens per model; every request is charged exactly (nano-dollar precision) from the account balance. Per-key credit limits, redeem codes, admin adjustments, signup bonus.
- **Web console** — users manage API keys, see usage, logs and spend charts, browse the model catalog and docs; admins manage providers (with connection test and one-click model import), models and endpoints, users, request logs and redeem codes. English and 简体中文, light and dark.
- **Easy to run** — one static binary, embedded SQLite (pure Go, no CGO), zero required config. Docker image and docker-compose included.
- **Secure defaults** — provider API keys are encrypted at rest (AES-256-GCM), only hashes of user API keys are stored, bcrypt passwords, HttpOnly session cookies, CSRF protection, login rate limiting.

## Quick start

### Docker

```bash
docker run -d --name plexrouter -p 8080:8080 -v $(pwd)/data:/data ghcr.io/clouly/plexrouter:latest
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
| `PLEXROUTER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

When running behind a reverse proxy (nginx, Caddy), disable response buffering for streaming — PlexRouter already sends `X-Accel-Buffering: no` for nginx.

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
- PostgreSQL support

Contributions are welcome — please open an issue to discuss larger changes first.

## License

[MIT](LICENSE)
