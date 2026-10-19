# Changelog

All notable changes to PlexRouter are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/) (before 1.0, minor versions may contain breaking changes).

## [Unreleased]

### Added

- **Zero-config start**: `docker run` and open the page. A setup wizard connects GitHub or Google sign-in (showing the URLs to register for the OAuth app); the first person to sign in becomes the admin.
- **Admin → System settings**: site name, public URL, sign-in providers, registration, signup credit, default rate limit, request timeout, log retention and proxy trust, all changed at runtime without restarting.

### Changed

- Environment variables are optional. A value saved in the console overrides its variable; deployments configured through the environment keep working unchanged.
- An account created while no admin exists becomes the admin (previously: the very first account). `PLEXROUTER_ADMIN_EMAILS` still takes precedence when set.

## [0.2.0] - 2026-10-19

### ⚠️ Upgrading from 0.1.0

- **Sign-in is now Google/GitHub only.** Email/password registration and login are removed. Configure at least one provider (`PLEXROUTER_GITHUB_CLIENT_ID`/`_SECRET` or `PLEXROUTER_GOOGLE_CLIENT_ID`/`_SECRET`, see the README) before upgrading. Existing accounts are kept: sign in with a Google or GitHub account that has the **same verified email** and it links to the old account automatically.
- **Admin bootstrap changed.** `PLEXROUTER_ADMIN_EMAIL` and `PLEXROUTER_ADMIN_PASSWORD` are replaced by `PLEXROUTER_ADMIN_EMAILS` (comma-separated). Listed emails become admins when they sign in.
- **docker-compose stores data in a named volume** (`plexrouter_data`) instead of the `./data` folder. If you used the 0.1.0 compose file, either keep your old mount (`- ./data:/data` under `volumes:`) or copy the data into the volume before starting 0.2.0:
  ```bash
  docker compose down
  docker run --rm -v "$PWD/data":/from -v plexrouter_data:/to alpine sh -c 'cp -a /from/. /to/ && chown -R 10001 /to'
  docker compose up -d
  ```
- **The Docker image moved** to `ghcr.io/local4a/plexrouter` (the project now lives in the [local4a](https://github.com/local4a) organization).
- Database migrations run automatically on start. Provider API keys are moved into the new key pool. Back up the data volume first.

### Added

- **Anthropic Messages API** (`POST /v1/messages`, `/v1/messages/count_tokens`) for Claude Code and Anthropic SDKs. Anthropic upstreams get requests byte-for-byte (all `anthropic-*` headers, unmodified streams and error bodies); every other provider is translated to and from the OpenAI format, including tool use, thinking and streaming.
- **Model-name aliases**: provider-style names such as `claude-sonnet-4-5-20250929` or `gpt-4o` resolve to catalog models.
- **Reasoning** (`reasoning_effort`, `reasoning: {effort, max_tokens, exclude}`) mapped to Anthropic thinking and Gemini `thinkingConfig`.
- **Prompt caching**: `cache_control` forwarded to Anthropic, cache reads/writes reported in `usage`, and billed at per-model cache prices.
- **Structured outputs** (`json_schema`) for Anthropic.
- **Provider key pools**: several upstream keys per provider, rotated; keys that are rejected or out of credit are switched off automatically, rate-limited keys cool down.
- **Failover**: a circuit breaker skips failing endpoints; provider health over the last 24 hours.
- **Operator reports**: upstream cost per endpoint, revenue/cost/profit by day, model and provider; alerts on the admin overview.
- **Catalog import** from OpenRouter's public model list with name matching and a markup percentage.
- **Limits**: requests-per-minute per user (`PLEXROUTER_DEFAULT_USER_RPM`) and per key; keys can be restricted to a list of models.
- **Sign in with Google / GitHub**, with linking of accounts by verified email.
- **Console in 10 languages**: English, 简体中文, 繁體中文, 日本語, 한국어, Español, Français, Deutsch, Português (Brasil), Русский.
- Request-log retention (`PLEXROUTER_LOG_RETENTION_DAYS`, default 90).
- `/healthz` checks the database.

### Changed

- The console's login page only offers Google/GitHub.
- CI builds the Docker image on every push.

## [0.1.0] - 2026-10-19

First release: OpenAI-compatible gateway (`/v1/chat/completions`, `/v1/embeddings`, `/v1/models`) with adapters for OpenAI(-compatible), Anthropic and Gemini; model routing with fallback; API keys, prepaid credits and per-token billing; redeem codes; web console; SQLite; Docker image.

[Unreleased]: https://github.com/local4a/plexrouter/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/local4a/plexrouter/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/local4a/plexrouter/releases/tag/v0.1.0
