# Console API

The web console talks to the server through a small JSON API under `/api/`.
It is separate from the OpenAI-compatible gateway API under `/v1/` (also served at `/api/v1/`).

## Conventions

- Requests and responses are JSON (`Content-Type: application/json`).
- Auth uses an HttpOnly session cookie (`plex_session`) set by login/register. Send requests with `credentials: "include"` (same origin).
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
  has_password: boolean;     // false for accounts created via Google/GitHub that never set one
  created_at: string;
};

type ApiKey = {
  id: number; name: string;
  prefix: string;            // e.g. "sk-plex-3f9a…" — display only
  credit_limit: number | null; // USD, null = unlimited (still bounded by account balance)
  usage: number;             // USD spent through this key
  disabled: boolean;
  created_at: string; last_used_at: string | null;
};

type PublicModel = {
  id: string;                // slug, e.g. "openai/gpt-4o" — what clients put in "model"
  name: string; description: string;
  context_length: number;
  input_price: number;       // USD / 1M prompt tokens
  output_price: number;      // USD / 1M completion tokens
  providers: number;         // how many upstream endpoints serve it
};

type UsageLog = {
  id: number;
  model: string;             // slug requested
  provider: string;          // provider name that served it ("" if none)
  api_key_name: string;
  prompt_tokens: number; completion_tokens: number;
  cost: number;              // USD
  latency_ms: number;
  status: "success" | "error";
  error: string;             // "" on success
  stream: boolean;
  created_at: string;
  user_email?: string;       // admin log endpoint only
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
  api_key_hint: string;      // masked, e.g. "sk-…abcd"; "" if none set
  enabled: boolean;
  created_at: string;
};

type Endpoint = {
  id: number; provider_id: number; provider_name: string;
  upstream_model: string;    // model name sent to the provider
  priority: number;          // lower = tried first
  enabled: boolean;
};

type AdminModel = {
  id: number; slug: string; name: string; description: string;
  context_length: number; input_price: number; output_price: number;
  enabled: boolean; created_at: string;
  endpoints: Endpoint[];
};

type RedeemCode = {
  id: number; code: string; amount: number;
  used_by_email: string | null; used_at: string | null;
  created_at: string;
};

type DailyStat = { date: string /* YYYY-MM-DD */; requests: number; tokens: number; cost: number };
```

## Public

| Method | Path | Body / Query | Response |
|---|---|---|---|
| GET | `/api/config` | | `{ name: string, version: string, allow_register: boolean, oauth_providers: ("google" \| "github")[] }` — `oauth_providers` lists only the ones configured on the server |
| GET | `/api/models` | | `{ data: PublicModel[] }` (enabled models only) |

## Auth

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/auth/register` | `{ email, password, name? }` | `{ user: User }` (+ sets cookie). Password ≥ 8 chars. The first user ever registered becomes admin. |
| POST | `/api/auth/login` | `{ email, password }` | `{ user: User }` (+ sets cookie) |
| POST | `/api/auth/logout` | | `{ ok: true }` |
| GET | `/api/me` | | `{ user: User }` or 401 |
| POST | `/api/me/password` | `{ old_password, new_password }` | `{ ok: true }`. When `has_password` is false, `old_password` is not required (this *sets* a first password). |
| GET | `/api/me/identities` | | `{ data: { provider: "google" \| "github", email: string, created_at: string }[] }` — linked third-party accounts |
| DELETE | `/api/me/identities/{provider}` | | `{ ok: true }`; `400 last_login_method` if the account has no password and no other linked identity |

### Google / GitHub sign-in (OAuth)

These are **browser navigations, not fetch calls** — set `window.location.href`:

- `GET /api/auth/oauth/{provider}?mode=login` — sign in or sign up (provider = `google` | `github`). On success the server sets the session cookie and redirects to `/`. A brand-new account is created when no linked identity or verified matching email exists (subject to registration being open; the very first account ever becomes admin). An existing account with the same **verified** email is linked automatically.
- `GET /api/auth/oauth/{provider}?mode=link` — while logged in: link that provider to the current account. On success redirects to `/settings?linked={provider}`.
- On failure the server redirects to `/login?oauth_error=<code>` (login mode) or `/settings?oauth_error=<code>` (link mode). Codes:
  `not_configured`, `state_mismatch` (expired/forged attempt — just retry), `provider_error` (the provider refused or is unreachable), `access_denied` (user cancelled on the provider's page), `email_unverified` (provider gave no verified email), `registration_closed`, `account_disabled`, `already_linked` (that Google/GitHub account belongs to another user), `not_logged_in` (link mode without a session).

## Error codes worth translating

The console should show a localized message for these `error.code` values and fall back to `error.message` for anything else (e.g. `invalid_request` details meant for admins):
`unauthorized`, `forbidden`, `not_found`, `internal_error`, `rate_limited`, `invalid_credentials` (wrong email or password), `wrong_password` (current password incorrect when changing it), `invalid_email`, `weak_password`, `email_taken`, `registration_closed`, `account_disabled`, `invalid_code` (redeem code invalid or used), `slug_taken`, `last_login_method`, `cross_origin`, plus the OAuth codes above.

## User (logged in)

| Method | Path | Body / Query | Response |
|---|---|---|---|
| GET | `/api/keys` | | `{ data: ApiKey[] }` |
| POST | `/api/keys` | `{ name, credit_limit?: number \| null }` | `{ key: ApiKey, secret: string }` — `secret` is shown only once |
| PATCH | `/api/keys/{id}` | `{ name?, credit_limit?: number \| null, disabled? }` | `{ key: ApiKey }` |
| DELETE | `/api/keys/{id}` | | `{ ok: true }` |
| GET | `/api/usage/logs` | `page, page_size, model?` | `{ data: UsageLog[], total }` |
| GET | `/api/usage/stats` | `days?` (default 7, max 90) | `{ total_cost, total_requests, total_tokens, daily: DailyStat[], by_model: {model, requests, tokens, cost}[] }` — `daily` has one entry per day, zero-filled, oldest first |
| GET | `/api/credits/transactions` | `page, page_size` | `{ data: Transaction[], total }` |
| POST | `/api/credits/redeem` | `{ code }` | `{ amount: number, balance: number }` |

## Admin (role = admin)

| Method | Path | Body / Query | Response |
|---|---|---|---|
| GET | `/api/admin/stats` | | `{ users, providers, models, total_balance, requests_today, cost_today, daily: DailyStat[] /* 14 days */ }` |
| GET | `/api/admin/providers` | | `{ data: Provider[] }` |
| POST | `/api/admin/providers` | `{ name, type, base_url?, api_key, enabled? }` | `{ provider: Provider }` |
| PATCH | `/api/admin/providers/{id}` | `{ name?, type?, base_url?, api_key?, enabled? }` — omit or `""` api_key to keep the current one | `{ provider: Provider }` |
| DELETE | `/api/admin/providers/{id}` | | `{ ok: true }` (also deletes its endpoints) |
| POST | `/api/admin/providers/{id}/test` | | `{ ok: boolean, latency_ms: number, message: string }` |
| GET | `/api/admin/providers/{id}/upstream-models` | | `{ data: { id: string }[] }` — models the provider reports |
| GET | `/api/admin/models` | | `{ data: AdminModel[] }` (all, incl. disabled) |
| POST | `/api/admin/models` | `{ slug, name, description?, context_length?, input_price, output_price, enabled?, endpoints: {provider_id, upstream_model, priority?, enabled?}[] }` | `{ model: AdminModel }` |
| PATCH | `/api/admin/models/{id}` | same fields, all optional; if `endpoints` is present it **replaces** the list | `{ model: AdminModel }` |
| DELETE | `/api/admin/models/{id}` | | `{ ok: true }` |
| POST | `/api/admin/models/import` | `{ provider_id, upstream_models: string[], slug_prefix: string }` | `{ created: number, skipped: number }` — creates disabled models `slug_prefix/upstream` with price 0; existing slugs get the endpoint added instead |
| GET | `/api/admin/users` | `page, page_size, q?` (email/name search) | `{ data: (User & { total_cost: number, requests: number })[], total }` |
| PATCH | `/api/admin/users/{id}` | `{ name?, role?, disabled? }` | `{ user: User }` |
| POST | `/api/admin/users/{id}/credits` | `{ amount: number /* may be negative */, note? }` | `{ user: User }` |
| GET | `/api/admin/logs` | `page, page_size, user_id?, model?` | `{ data: UsageLog[] /* with user_email */, total }` |
| GET | `/api/admin/redeem-codes` | `page, page_size` | `{ data: RedeemCode[], total }` |
| POST | `/api/admin/redeem-codes` | `{ amount: number, count: number /* 1-100 */ }` | `{ codes: string[] }` |
| DELETE | `/api/admin/redeem-codes/{id}` | | `{ ok: true }` (unused codes only) |

## Gateway API (for API clients, documented in the console's "Docs" page)

- Base URL: `<origin>/v1` (also `<origin>/api/v1`). Auth: `Authorization: Bearer sk-plex-...`.
- `POST /v1/chat/completions` — OpenAI Chat Completions format, `stream: true` supported (SSE).
  Extra fields: `models: string[]` (fallback list tried in order if the primary model fails), and the response `usage` includes `cost` (USD).
- `GET /v1/models` — OpenAI-style list: `{ object: "list", data: [{ id, object: "model", created, owned_by, name, context_length, pricing: { prompt: "<USD per token>", completion: "<USD per token>" } }] }`.
- `GET /v1/key` — info about the key in use: `{ data: { name, usage, limit, balance } }`.
