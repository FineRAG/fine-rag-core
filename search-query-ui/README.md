# Search Query UI

`search-query-ui` is a React + Vite frontend for tenant-scoped query and answer exploration.

## Features

- Tenant session bootstrap (`tenantId`, `requestId`, API key)
- Query submission to `/api/v1/search/stream`
- Incremental SSE answer rendering
- Citation list and trace metadata display
- Visible interruption/retry state for stream failures

## Local Run

```bash
npm ci
VITE_SEARCH_API_BASE_URL=http://localhost:8080 npm run dev
```

## Quality Gates

```bash
npm run lint
npm run test
npm run build
```

## Container Run

```bash
docker compose up -d search-query-ui
```

Served at `http://localhost:14174`.
