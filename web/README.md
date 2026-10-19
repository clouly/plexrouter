# PlexRouter web console

React 18 + TypeScript + Vite + Tailwind v4 single-page app for the PlexRouter console.
The API contract it talks to is documented in [`../docs/console-api.md`](../docs/console-api.md).

## Development

Start the Go backend on `:8080`, then:

```sh
npm install
npm run dev
```

Vite proxies `/api` and `/v1` to the backend (override the target with
`PLEXROUTER_BACKEND=http://host:port npm run dev`).

## Build

```sh
npm run build
```

Type-checks and writes the static site to `dist/` (committed, so the backend can embed/serve it
without a Node toolchain). Serve `dist/index.html` for any unknown non-API path (SPA routing).

## Notes

- UI strings live in `src/i18n/en.ts` (source of truth) and `src/i18n/zh-CN.ts`; the TypeScript
  types fail the build if a key is missing from either.
- No runtime dependencies beyond React and react-router.
