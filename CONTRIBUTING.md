# Contributing to PlexRouter

Thanks for helping! Bug reports, fixes, provider adapters, translations and docs are all welcome.

- **Bugs and ideas**: open an [issue](https://github.com/local4a/plexrouter/issues/new/choose). For larger changes, please open an issue first, so we can agree on the approach before you write code.
- **Security problems**: don't open an issue; see [SECURITY.md](SECURITY.md).
- **Testing against real providers**: many of us can only test some providers. If you run PlexRouter against a provider and something breaks, an issue with the request (without your keys) and the upstream's response is very valuable.

## Development setup

You need Go 1.23+ and Node.js 20+.

```bash
git clone https://github.com/local4a/plexrouter.git
cd plexrouter
make build      # console into web/dist, server into bin/
make test       # Go tests
```

For working on the console with hot reload:

```bash
make run        # backend on :8080
make dev-web    # console on :5173, proxied to the backend
```

Signing in needs a GitHub (or Google) OAuth app; for local development create a GitHub OAuth app with the callback `http://localhost:8080/api/auth/oauth/github/callback` and set `PLEXROUTER_GITHUB_CLIENT_ID` / `PLEXROUTER_GITHUB_CLIENT_SECRET`. Sign in on port 8080 (the OAuth callback points there); the session cookie then works on the dev server at 5173 as well.

## Project layout

```
cmd/plexrouter/     entry point
internal/config/    environment configuration
internal/store/     SQLite persistence and migrations
internal/provider/  upstream adapters (OpenAI-compatible, Anthropic, Gemini) and format conversion
internal/gateway/   /v1 API: auth, routing, key pools, failover, limits, billing, Messages API
internal/server/    console JSON API, OAuth sign-in, embedded console
internal/catalog/   OpenRouter model catalog import
web/                React + Vite + Tailwind console
docs/               console API contract (docs/console-api.md)
```

## Guidelines

- **Tests**: add or update tests for behaviour changes. The server tests in `internal/server` run end to end against fake upstreams (see `fakeOpenAI`, `fakeAnthropic`, `fakeGemini`); add a fake for a new provider the same way.
- **Formatting**: `make lint` (gofmt + go vet) must pass. The console must build with `cd web && npm run build` (TypeScript strict).
- **Database changes**: append a new migration to the list in `internal/store/db.go`; never edit a released one.
- **API changes**: keep `docs/console-api.md` in sync with the console API.
- **Translations**: the console has 10 languages in `web/src/i18n/locales/`. `en.ts` is the source of truth; every other locale must have every key (the build fails otherwise). If you add a string, add it to all of them — a rough translation is fine, native speakers can improve it later. Fixes to existing translations are very welcome.
- **Commits and PRs**: keep PRs focused; describe what changed and how you tested it. Update `CHANGELOG.md` under an "Unreleased" heading for user-visible changes.

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
