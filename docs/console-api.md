# Console API

The web console talks to the server through a small JSON API under `/api/`.
It is separate from the OpenAI-compatible gateway API under `/v1/` (also served at `/api/v1/`).

## Conventions

- Requests and responses are JSON (`Content-Type: application/json`).
- Auth uses an HttpOnly session cookie (`plex_session`) set after signing in with Google or GitHub (see below). Send requests with `credentials: "include"` (same origin).
- Errors: non-2xx status with body `{"error": {"message": "human readable", "code": "snake_case"}}`.
  `401` means not logged in, `403` means not an admin.
- Money is always a JSON number in **USD** (e.g. `12.5`). Model prices are **USD per 1M tokens**.
- Timestamps are RFC 3339 strings (`"2026-10-19T11:08:00Z"`); nullable timestamps are `null`.
- Paginated lists take `?page=1&page_size=20` (page is 1-based, page_size max 100) and return `{"data": [...], "total": 123}`.
- IDs are integers unless noted.

## Objects

```ts
type User = {
  id: number; email: string; name: string;
  role: "admin" | "user";
  balance: number;           // USD
  disabled: boolean;
  rate_limit_rpm: number | null; // requests/minute across all the user's keys; null = server default (PLEXROUTER_DEFAULT_USER_RPM, 0 = unlimited)
  created_at: string;
};

type ApiKey = {
  id: number; name: string;
  prefix: string;            // e.g. "sk-plex-3f9a…" — display only
  credit_limit: number | null; // USD, null = unlimited (still bounded by account balance)
  usage: number;             // USD spent through this key
  rate_limit_rpm: number | null; // requests/minute for this key; null = no key-level limit
  allowed_models: string[];  // model slugs this key may call; [] = all models
  disabled: boolean;
  created_at: string; last_used_at: string | null;
};

type PublicModel = {
  id: string;                // slug, e.g. "openai/gpt-4o" — what clients put in "model"
  name: string; description: string;
  context_length: number;
  input_price: number;       // USD / 1M prompt tokens
  output_price: number;      // USD / 1M completion tokens
  cache_read_price: number | null;  // USD / 1M cached prompt tokens read; null = charged as input
  cache_write_price: number | null; // USD / 1M prompt tokens written to cache; null = charged as input
  providers: number;         // how many upstream endpoints serve it
};

type UsageLog = {
  id: number;
  model: string;             // slug requested
  provider: string;          // provider name that served it ("" if none)
  api_key_name: string;
  prompt_tokens: number; completion_tokens: number; // prompt_tokens includes cached tokens
  cache_read_tokens: number; cache_write_tokens: number;
  api: "chat" | "messages" | "embeddings"; // which gateway endpoint was called
  cost: number;              // USD
  latency_ms: number;
  status: "success" | "error";
  error: string;             // "" on success
  stream: boolean;
  created_at: string;
  user_email?: string;       // admin log endpoint only
  upstream_cost?: number | null; // admin only: USD paid to the provider; null = endpoint has no cost configured
  provider_key?: string;     // admin only: name of the provider key used ("" if none)
};

type Transaction = {
  id: number; amount: number; // USD, positive = credit added
  type: "signup" | "redeem" | "adjust";
  note: string; created_at: string;
};

type Provider = {
  id: number; name: string;
  type: "openai" | "anthropic" | "gemini" | "openai_compatible";
  base_url: string;          // "" = the type's default
  enabled: boolean;
  keys_total: number;        // provider keys configured
  keys_active: number;       // keys currently usable (enabled, not failed, not cooling down)
  health: ProviderHealth;
  created_at: string;
};

// Computed from the last 24 h of requests.
type ProviderHealth = {
  requests_24h: number;
  success_rate_24h: number | null; // 0..1, null when no requests
  avg_latency_ms_24h: number | null;
  status: "healthy" | "degraded" | "down" | "idle" | "no_keys";
  // healthy: success ≥ 95 %; degraded: 80–95 %; down: < 80 % (with ≥ 10 requests);
  // idle: < 10 requests; no_keys: no usable key (and the type needs one)
};

// One upstream API key of a provider. A provider can have many; requests rotate across usable keys.
type ProviderKey = {
  id: number; provider_id: number;
  name: string;              // label, e.g. "team account #2"
  key_hint: string;          // masked, e.g. "sk-…abcd"
  enabled: boolean;          // admin switch
  status: "active" | "auth_failed" | "quota_exceeded" | "cooling_down" | "disabled";
  // auth_failed / quota_exceeded are set automatically on upstream 401/403 or out-of-credit
  // errors and stop the key from being used until an admin re-enables it.
  // cooling_down: temporarily skipped after a 429 rate-limit (see cooldown_until).
  last_error: string;        // "" if none
  last_error_at: string | null;
  cooldown_until: string | null;
  last_used_at: string | null;
  requests_24h: number; errors_24h: number;
  created_at: string;
};

type Endpoint = {
  id: number; provider_id: number; provider_name: string;
  upstream_model: string;    // model name sent to the provider
  priority: number;          // lower = tried first
  enabled: boolean;
  input_cost: number | null; // USD / 1M tokens the operator pays this provider; null = unknown
  output_cost: number | null;
  cache_read_cost: number | null;  // null = same as input_cost
  cache_write_cost: number | null; // null = same as input_cost
  circuit_open_until: string | null; // set while the endpoint is skipped after repeated failures
};

type AdminModel = {
  id: number; slug: string; name: string; description: string;
  context_length: number; input_price: number; output_price: number;
  cache_read_price: number | null; cache_write_price: number | null; // null = charged as input
  enabled: boolean; created_at: string;
  endpoints: Endpoint[];
};

type RedeemCode = {
  id: number; code: string; amount: number;
  used_by_email: string | null; used_at: string | null;
  created_at: string;
};

type SystemSettings = {
  site_name: string;
  public_url: string;          // "" = derive from each request
  allow_register: boolean;     // create accounts for new Google/GitHub users
  signup_credit: number;       // USD for new accounts
  default_user_rpm: number;    // requests/minute per user unless set on the user; 0 = unlimited
  request_timeout_seconds: number;
  log_retention_days: number;  // 0 = keep forever
  trust_proxy: boolean;        // read client IPs from X-Forwarded-For
  github: { client_id: string; client_secret_set: boolean };
  google: { client_id: string; client_secret_set: boolean };
};
// PATCH body: the same scalar fields, plus { github?: { client_id?, client_secret? }, google?: {...} }.
// A provider's client_id "" disables it; null drops the console value (back to the env variable, if any).
// The server refuses (400 no_sign_in_provider) a change that would leave no sign-in provider.

type DailyStat = { date: string /* YYYY-MM-DD */; requests: number; tokens: number; cost: number };

// Model from the public OpenRouter catalog, used as a reference for importing.
type CatalogModel = {
  id: string;                // e.g. "openai/gpt-4o" — becomes our slug
  name: string; description: string; context_length: number;
  input_price: number; output_price: number; // reference USD / 1M tokens
  cache_read_price: number | null; cache_write_price: number | null;
  upstream_match: string | null; // with ?provider_id: the provider's model that matches, else null
  exists: boolean;           // a model with this slug already exists here
};

type Alert = {
  level: "warning" | "error";
  kind: "key_failed" | "provider_no_keys" | "provider_down" | "model_unroutable" | "missing_cost";
  message: string;           // English fallback
  params: { provider?: string; key?: string; reason?: "auth" | "quota"; model?: string; rate?: number /* % */; n?: number };
                             // values for a localized message, by kind
  provider_id?: number; model_id?: number; key_id?: number;
};

type ReportRow = { requests: number; tokens: number; revenue: number; upstream_cost: number; profit: number; unknown_cost_requests: number };
// revenue = what users were charged; upstream_cost = what the operator paid providers (only for
// endpoints with costs set); profit = revenue − upstream_cost over requests with known cost;
// unknown_cost_requests = successful requests whose endpoint had no cost configured.
```

## Public

| Method | Path | Body / Query | Response |
|---|---|---|---|
| GET | `/api/config` | | `{ name: string, version: string, allow_register: boolean, oauth_providers: ("google" \| "github")[], setup_required: boolean }` — `oauth_providers` lists only the configured ones. `setup_required` is true until an admin account exists: the console then shows the setup wizard instead of everything else (except Models and Docs) |

## First-run setup

A fresh instance has no configuration. While `setup_required` is true, anyone who opens the console can run the setup wizard (there is no setup code — the first visitor sets the instance up), then the **first person to sign in becomes the admin**.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/setup` | | `{ setup_required: boolean, suggested_public_url: string /* from the request */, callback_urls: { github: string, google: string } /* for suggested_public_url */ }` |
| POST | `/api/setup` | `{ site_name?: string, public_url?: string, github?: { client_id, client_secret }, google?: { client_id, client_secret } }` — at least one provider required | `{ ok: true, oauth_providers: ("google" \| "github")[] }`. `403 setup_done` once an admin exists. `400 invalid_request` if no provider is given or a value is malformed. May be called again (e.g. to fix a typo) until someone has signed in as admin |

After a successful POST, the wizard sends the browser to `/api/auth/oauth/<provider>?mode=login`; that sign-in creates the admin.
| GET | `/api/models` | | `{ data: PublicModel[] }` (enabled models only) |

## Auth

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/auth/logout` | | `{ ok: true }` |
| GET | `/api/me` | | `{ user: User }` or 401 |
| GET | `/api/me/identities` | | `{ data: { provider: "google" \| "github", email: string, created_at: string }[] }` — linked third-party accounts |
| DELETE | `/api/me/identities/{provider}` | | `{ ok: true }`; `400 last_login_method` if it is the account's only linked identity (the only way to sign in) |

### Google / GitHub sign-in (OAuth)

These are **browser navigations, not fetch calls** — set `window.location.href`:

- `GET /api/auth/oauth/{provider}?mode=login` — sign in or sign up (provider = `google` | `github`). On success the server sets the session cookie and redirects to `/`. A brand-new account is created when no linked identity or verified matching email exists (subject to registration being open; emails in `PLEXROUTER_ADMIN_EMAILS` become admins and may always sign up; without that list the very first account becomes admin). An existing account with the same **verified** email is linked automatically.
- `GET /api/auth/oauth/{provider}?mode=link` — while logged in: link that provider to the current account. On success redirects to `/settings?linked={provider}`.
- On failure the server redirects to `/login?oauth_error=<code>` (login mode) or `/settings?oauth_error=<code>` (link mode). Codes:
  `not_configured`, `state_mismatch` (expired/forged attempt — just retry), `provider_error` (the provider refused or is unreachable), `access_denied` (user cancelled on the provider's page), `email_unverified` (provider gave no verified email), `registration_closed`, `account_disabled`, `already_linked` (that Google/GitHub account belongs to another user), `not_logged_in` (link mode without a session).

## Error codes worth translating

The console should show a localized message for these `error.code` values and fall back to `error.message` for anything else (e.g. `invalid_request` details meant for admins):
`unauthorized`, `forbidden`, `not_found`, `internal_error`, `rate_limited`, `registration_closed`, `account_disabled`, `invalid_code` (redeem code invalid or used), `slug_taken`, `last_login_method`, `cross_origin`, `catalog_unavailable`, plus the OAuth codes above.

## User (logged in)

| Method | Path | Body / Query | Response |
|---|---|---|---|
| GET | `/api/keys` | | `{ data: ApiKey[] }` |
| POST | `/api/keys` | `{ name, credit_limit?: number \| null, rate_limit_rpm?: number \| null, allowed_models?: string[] }` | `{ key: ApiKey, secret: string }` — `secret` is shown only once |
| PATCH | `/api/keys/{id}` | `{ name?, credit_limit?: number \| null, rate_limit_rpm?: number \| null, allowed_models?: string[], disabled? }` | `{ key: ApiKey }` |
| DELETE | `/api/keys/{id}` | | `{ ok: true }` |
| GET | `/api/usage/logs` | `page, page_size, model?` | `{ data: UsageLog[], total }` |
| GET | `/api/usage/stats` | `days?` (default 7, max 90) | `{ total_cost, total_requests, total_tokens, daily: DailyStat[], by_model: {model, requests, tokens, cost}[] }` — `daily` has one entry per day, zero-filled, oldest first |
| GET | `/api/credits/transactions` | `page, page_size` | `{ data: Transaction[], total }` |
| POST | `/api/credits/redeem` | `{ code }` | `{ amount: number, balance: number }` |

## Admin (role = admin)

| Method | Path | Body / Query | Response |
|---|---|---|---|
| GET | `/api/admin/settings` | | `{ settings: SystemSettings, sources: Record<keyof SystemSettings, "console" \| "env" \| "default">, callback_urls: { github: string, google: string } }` — `sources` says where each current value comes from (console value > environment variable > built-in default) |
| PATCH | `/api/admin/settings` | any subset of `SystemSettingsInput` — `null` resets a field to its env/default value; a secret sent as `""` keeps the current one | `{ settings, sources, callback_urls }` |
| GET | `/api/admin/stats` | | `{ users, providers, models, total_balance, requests_today, cost_today /* = revenue today */, profit_today, daily: DailyStat[] /* 14 days */, alerts: Alert[] }` |
| GET | `/api/admin/report` | `days?` (default 30, max 365) | `{ totals: ReportRow, daily: (ReportRow & { date: string })[] /* zero-filled, oldest first */, by_model: (ReportRow & { model: string })[], by_provider: (ReportRow & { provider_id: number, provider: string, success_rate: number \| null })[] }` |
| GET | `/api/admin/providers` | | `{ data: Provider[] }` |
| POST | `/api/admin/providers` | `{ name, type, base_url?, api_key?, enabled? }` — `api_key`, if given, becomes the provider's first key | `{ provider: Provider }` |
| PATCH | `/api/admin/providers/{id}` | `{ name?, type?, base_url?, enabled? }` (keys are managed below) | `{ provider: Provider }` |
| GET | `/api/admin/providers/{id}/keys` | | `{ data: ProviderKey[] }` |
| POST | `/api/admin/providers/{id}/keys` | `{ api_keys: string[], name?: string }` — one or many keys (e.g. pasted one per line); with several, names get " #1", " #2"… | `{ data: ProviderKey[] /* the new ones */ }` |
| PATCH | `/api/admin/providers/{id}/keys/{keyId}` | `{ name?, enabled? }` — `enabled: true` also clears a failed status and cooldown | `{ key: ProviderKey }` |
| DELETE | `/api/admin/providers/{id}/keys/{keyId}` | | `{ ok: true }` |
| DELETE | `/api/admin/providers/{id}` | | `{ ok: true }` (also deletes its endpoints) |
| POST | `/api/admin/providers/{id}/test` | | `{ ok: boolean, latency_ms: number, message: string }` |
| GET | `/api/admin/providers/{id}/upstream-models` | | `{ data: { id: string }[] }` — models the provider reports |
| GET | `/api/admin/models` | | `{ data: AdminModel[] }` (all, incl. disabled) |
| POST | `/api/admin/models` | `{ slug, name, description?, context_length?, input_price, output_price, cache_read_price?: number \| null, cache_write_price?: number \| null, enabled?, endpoints: {provider_id, upstream_model, priority?, enabled?, input_cost?, output_cost?, cache_read_cost?, cache_write_cost? /* number \| null */}[] }` | `{ model: AdminModel }` |
| PATCH | `/api/admin/models/{id}` | same fields, all optional; if `endpoints` is present it **replaces** the list | `{ model: AdminModel }` |
| DELETE | `/api/admin/models/{id}` | | `{ ok: true }` |
| POST | `/api/admin/models/import` | `{ provider_id, upstream_models: string[], slug_prefix: string }` | `{ created: number, skipped: number }` — creates disabled models `slug_prefix/upstream` with price 0; existing slugs get the endpoint added instead |
| GET | `/api/admin/catalog` | `provider_id?` | `{ data: CatalogModel[], fetched_at: string }` — the public OpenRouter model list (cached ~10 min, fetched by the server). With `provider_id`, `upstream_match` is filled by matching against that provider's model list. `502 catalog_unavailable` if it can't be fetched |
| POST | `/api/admin/catalog/import` | `{ provider_id, items: { id: string, upstream_model: string }[], markup_percent: number /* e.g. 20 = sell at 1.2× reference */, enable: boolean, update_existing: boolean }` | `{ created, updated, endpoints_added }` — new models get the catalog name/description/context and sell price = reference × (1 + markup/100); the endpoint's cost is set to the reference price. Existing slugs: the endpoint is added if missing; with `update_existing` their sell prices and metadata are refreshed too |
| GET | `/api/admin/users` | `page, page_size, q?` (email/name search) | `{ data: (User & { total_cost: number, requests: number })[], total }` |
| PATCH | `/api/admin/users/{id}` | `{ name?, role?, disabled?, rate_limit_rpm?: number \| null }` | `{ user: User }` |
| POST | `/api/admin/users/{id}/credits` | `{ amount: number /* may be negative */, note? }` | `{ user: User }` |
| GET | `/api/admin/logs` | `page, page_size, user_id?, model?` | `{ data: UsageLog[] /* with user_email */, total }` |
| GET | `/api/admin/redeem-codes` | `page, page_size` | `{ data: RedeemCode[], total }` |
| POST | `/api/admin/redeem-codes` | `{ amount: number, count: number /* 1-100 */ }` | `{ codes: string[] }` |
| DELETE | `/api/admin/redeem-codes/{id}` | | `{ ok: true }` (unused codes only) |

## Gateway API (for API clients, documented in the console's "Docs" page)

- Base URL: `<origin>/v1` (also `<origin>/api/v1`). Auth: `Authorization: Bearer sk-plex-...`.
- `POST /v1/chat/completions` — OpenAI Chat Completions format, `stream: true` supported (SSE).
  Extra fields: `models: string[]` (fallback list tried in order if the primary model fails), and the response `usage` includes `cost` (USD).
- `GET /v1/models` — OpenAI-style list: `{ object: "list", data: [{ id, object: "model", created, owned_by, name, context_length, pricing: { prompt: "<USD per token>", completion: "<USD per token>", input_cache_read?: "…", input_cache_write?: "…" } }] }`.
- `POST /v1/messages` — **Anthropic Messages API** (for Claude Code and Anthropic SDKs). Auth with `x-api-key: sk-plex-…` or `Authorization: Bearer sk-plex-…`. Works with any provider: Anthropic upstreams get the request as-is (incl. `anthropic-beta`, thinking, `cache_control`); other providers are translated to and from OpenAI format. Streaming supported. `POST /v1/messages/count_tokens` is also served (exact for Anthropic upstreams, estimated otherwise).
  Claude Code: `ANTHROPIC_BASE_URL=<origin>` and `ANTHROPIC_AUTH_TOKEN=sk-plex-…` (Claude Code appends `/v1/messages`), optionally `ANTHROPIC_MODEL=<model id>`.
- **Model names**: besides the exact id (`anthropic/claude-sonnet-4.5`), requests may use the provider's own name (`claude-sonnet-4-5-20250929`, `gpt-4o`): the gateway resolves it to the matching model, ignoring the vendor prefix, dots vs dashes and date suffixes.
- **Reasoning** (chat completions): `reasoning_effort: "minimal"|"low"|"medium"|"high"` (OpenAI style) or `reasoning: { effort?, max_tokens?, exclude? }` (OpenRouter style). Mapped to Anthropic `thinking.budget_tokens` and Gemini `thinkingConfig`. Reasoning text is returned in `message.reasoning` / `delta.reasoning` (omitted with `exclude: true`).
- **Prompt caching**: put `cache_control: {"type": "ephemeral"}` on a content part (system or user message) to mark a cache breakpoint for Anthropic; OpenAI and Gemini cache automatically. `usage.prompt_tokens_details.cached_tokens` and `usage.prompt_tokens_details.cache_write_tokens` report cache use, and cached tokens are billed at the model's cache prices.
- **Structured outputs**: `response_format: {type: "json_schema", json_schema: {name, schema}}` works for all providers (Anthropic via a forced tool call; the JSON is returned as message content).
- Limits: `429 rate_limited` (with a `Retry-After` header, seconds) when the key's or the user's requests-per-minute limit is hit; `403 model_not_allowed` when the key's `allowed_models` excludes the requested model.
- `GET /v1/key` — info about the key in use: `{ data: { name, usage, limit, balance } }`.
