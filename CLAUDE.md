# Contributing to firebin-mcp

This guide is for anyone opening a pull request here, including people driving the change through Claude. Read it before you write code; it is short on purpose.

## What this service is

The FireBin MCP server. Go 1.26, the `modelcontextprotocol/go-sdk`, standard-library `net/http`. It presents a Model Context Protocol surface over streamable HTTP and translates each call into an authenticated request against `firebin-api`. It has no database access and no business logic of its own; keep it that way.

## Layout

- `cmd/mcp/main.go` wires config, the API client, the tool and resource registries, and the HTTP server.
- `internal/config/config.go` parses the environment and resolves the inbound token: env, then the persisted file, then a first-run generate.
- `internal/api/client.go` is the only place that speaks HTTP to FireBin. Generic `Get`, `Post`, and `Patch` over bare JSON, with the `{error, code}` envelope decoded into a typed `*api.Error`.
- `internal/tools/` holds one file per domain plus `tools.go`, whose package comment is the catalogue and the record of what is deliberately excluded.
- `internal/tools/wire.go` holds the `api*` structs that mirror the API wire shapes, and the trimmed structs handed to the model.
- `internal/resources/` holds the `firebin://` URIs.

## Conventions that matter

- Every tool projects. Never hand a raw `api*` struct back to the caller: a `Part` carries its variants, parameters, manufacturer parts, supplier SKUs, and price breaks inline, and the API paginates nothing, so an unprojected list can be the whole inventory.
- Every list tool caps its output and reports `total` and `truncated`. A silent truncation reads as a complete answer.
- Write the tool description for a model that has never seen FireBin. Say what the tool answers, name the tool to call first to get the ids it needs, and say what it will not do. `search_parts` says the match is a substring test because otherwise a model writes sentences into it.
- Adding a tool means adding it to `RegisterAll` and to the catalogue in the `tools.go` package comment. Excluding one means writing down why, in the same comment.
- `PATCH` on the API is a full-object replacement decoded with `DisallowUnknownFields`. Read the current object, map it into the request struct field by field, then apply the caller's changes. Echoing back a fetched model is a 400, and a dropped field silently wipes data.
- After a write that the API answers with a partial object, re-read before returning. A response missing `manufacturer_parts` reads as though the write destroyed them.
- New `.go` files start with the SPDX header the rest of the tree uses: `// SPDX-License-Identifier: AGPL-3.0-only` and the copyright line.
- No secret is ever logged. The startup banner prints the generated inbound token exactly once, by design; nothing else prints a credential.

## Before you open a pull request

Run what CI runs:

```sh
go build ./...
go vet ./...
go test -race ./...
```

CI also runs golangci-lint. The action major and the linter version are pinned together in `.github/workflows/ci.yml`: an action below v8 installs a golangci-lint v1 binary, which rejects the v2 `.golangci.yml` with "unsupported version of the configuration".

Keep commits small and focused, and write a message that says what changed and why. Do not add `Co-Authored-By` or "Generated with" trailers; the commit is authored by the person who sent it.
