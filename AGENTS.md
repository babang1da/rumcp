# rumcp — agent rules

Hosted MCP connectors to Russian services. Go server: `server/` (entry `server/cmd/`); web: `web/` (Vite).

- Gate before PR: `cd server && go vet ./... && go test -race ./...`; web: `cd web && npm ci && npm run build && npm test`.
- Branch from `develop`, PR to `develop`, title `ZHC-N: ...`.
- No secrets or tokens in files, commits or issues.
- Prefix shell commands with `rtk`.
