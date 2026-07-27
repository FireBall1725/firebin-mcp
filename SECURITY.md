# Security policy

## Reporting a vulnerability

Report privately through [GitHub Security Advisories](https://github.com/FireBall1725/firebin-mcp/security/advisories/new) rather than opening a public issue. Include what you did, what happened, and what you expected.

This is a single-maintainer alpha project. Expect an acknowledgement within a week.

## What this server holds

Two credentials, both bearer tokens:

- **`FIREBIN_ACCESS_TOKEN`**, the outbound `fbin_pat_` personal access token. It carries the full authority of the FireBin account that minted it. FireBin stores per-token scopes but no middleware reads them, so the account's role is the only thing limiting what this server can do. Mint it from a dedicated non-admin account.
- **`FIREBIN_MCP_TOKEN`**, the inbound token every MCP client must present. Generated on first run and written to `<data dir>/mcp-token` with mode 0600 if you do not supply one. Rotate it by deleting that file and restarting.

Neither token is logged. The generated inbound token is printed once, in the first-run banner, and never again.

## Deployment notes

- Terminate TLS in front of this server. Both tokens travel in an `Authorization` header, and nothing here speaks HTTPS on its own.
- `GET /health` is unauthenticated and returns only a status and a version string. Everything under `/mcp` requires the bearer token, compared in constant time.
- Give the container a persistent volume at the data directory. Without one, a restart generates a fresh inbound token and every configured client stops working.
- The image runs as the distroless `nonroot` user (uid 65532) and contains no shell.
