# firebin-mcp

Query and manage a [FireBin](https://github.com/FireBall1725/firebin) electronics parts inventory from Claude, Cursor, or any other MCP client. Ask what is in a drawer, look up distributor pricing for a part number, book a delivery in against a bin, or check whether you have the parts to build a board.

> **Alpha.** FireBin is early software and so is this. The tool surface will change. Run it against an instance whose data you can restore.

## Part of the FireBin stack

| Repo | What it is |
| --- | --- |
| [firebin](https://github.com/FireBall1725/firebin) | Docker Compose stack, self-host guide, project docs |
| [firebin-api](https://github.com/FireBall1725/firebin-api) | Go and Postgres backend, owns the schema and the jobs |
| [firebin-web](https://github.com/FireBall1725/firebin-web) | React client |
| **firebin-mcp** | Model Context Protocol server ← you are here |

## How it works

This server holds no database credentials. Every tool call turns into an HTTP request against the FireBin REST API carrying an `fbin_pat_` personal access token, so the MCP inherits the API's own authorization and validation rather than reimplementing them.

That has one consequence worth planning around: **a token carries the full authority of the account that minted it.** FireBin stores per-token scopes but no middleware reads them today, so scoping is done by choosing the role of the account. Mint the token from a dedicated `member` account and the API refuses user administration, instance settings, backup export and import, and empty-lot cleanup on its own. Mint it from a `viewer` account and every write tool below returns `403 read-only account`.

Traffic in the other direction is gated separately. Clients connecting to this server present an `fbin_mcp_` bearer token, checked with a constant-time compare.

## Tools

### Read

| Tool | What it answers |
| --- | --- |
| `search_parts` | Find parts by name, keywords, IPN, or manufacturer part number |
| `search_parts_by_spec` | Find parts by package and parameter value, with unit-aware matching: "220 ohm" never matches 220 pF |
| `get_part` | One part in full: specs, every MPN with distributor SKUs and price breaks, per-bin stock |
| `list_locations` | Every bin, drawer, and cabinet with its full path and barcode |
| `location_contents` | What is in one bin, by location id or scanned barcode |
| `low_stock` | The reorder list: parts at or below their minimum, with the shortfall |
| `inventory_stats` | Part, variant, location, and low-stock counts, total units, total value |
| `list_categories` | Categories with part counts, for resolving a name to an id |
| `stock_history` | Movements for one part: added, removed, counted, moved, with notes |
| `recent_activity` | The last movements across the whole inventory |
| `lookup_mpn` | Distributor data for a part number without writing anything |
| `list_projects` | Hardware projects and their board counts |
| `get_project` | One project with its boards, revisions, and BOM line counts |
| `get_board` | One board's bill of materials line by line, with the manufacturer part number on each |
| `pick_list` | Whether a board can be built, and what to pull from which bin |

### Write

| Tool | What it does |
| --- | --- |
| `scan_barcode` | Resolve a scanned code to a bin, a stock lot, or a distributor label |
| `adjust_stock` | Add, remove, count, or correct the quantity of a part |
| `move_stock` | Relocate units between bins without changing the count |
| `add_part_by_mpn` | Create a part from a manufacturer part number, enriched and optionally stocked |
| `update_part` | Change a part's name, package, category, or reorder threshold |
| `create_location` | Add a bin, optionally nested under an existing one |

Search matching is a case-insensitive substring test across name, keywords, IPN, and MPN. There is no ranking and no fuzzy matching, so one short distinctive term beats a phrase.

No tool in this server deletes anything. Labels, printing, part images, BOM upload, stock lot splitting and merging, the job queue, and every admin-gated route are out of scope; see the package comment in `internal/tools/tools.go` for why each one is excluded.

## Resources

| URI | Contents |
| --- | --- |
| `firebin://parts` | Every part with stock totals |
| `firebin://part/{id}` | One part in full |
| `firebin://locations` | Every storage location |
| `firebin://location/{id}` | The stock lots in one location |
| `firebin://categories` | Categories with part counts |
| `firebin://stats` | Inventory summary |

Resources pass the API's JSON through untrimmed, while tools project it down. A tool is called in a loop and has to stay cheap; a resource is pulled deliberately by a client that wants the whole row.

## Quick start

1. Mint a personal access token in the FireBin web UI under Settings, API tokens. Copy the `fbin_pat_` value; it is shown once.

2. Run the container next to your FireBin stack:

   ```yaml
   firebin-mcp:
     image: ghcr.io/fireball1725/firebin-mcp:latest
     restart: unless-stopped
     environment:
       FIREBIN_API_URL: http://firebin-api:8080
       FIREBIN_ACCESS_TOKEN: ${FIREBIN_ACCESS_TOKEN:?}
     ports:
       - "8090:8090"
     volumes:
       - mcp_data:/data
   ```

3. Read the inbound token out of the startup log. On first boot the server generates one, prints it in a banner, and writes it to `/data/mcp-token`. Set `FIREBIN_MCP_TOKEN` yourself to skip that.

4. Point your client at it:

   ```json
   {
     "mcpServers": {
       "firebin": {
         "type": "http",
         "url": "http://localhost:8090/mcp",
         "headers": { "Authorization": "Bearer fbin_mcp_..." }
       }
     }
   }
   ```

`GET /health` is unauthenticated and returns the running version, so probes and uptime monitors need no credential.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `FIREBIN_API_URL` | required | Base URL of the FireBin API. `/api/v1` is appended when absent, so the bare service URL works. |
| `FIREBIN_ACCESS_TOKEN` | required | The `fbin_pat_` token used on outbound calls. |
| `FIREBIN_MCP_TOKEN` | generated | Bearer token incoming clients must present. Resolved from this variable, then `<data dir>/mcp-token`, then generated on first run. |
| `FIREBIN_MCP_LISTEN` | `:8090` | Address to bind. |
| `FIREBIN_MCP_DATA_DIR` | `/data` | Where the generated token is persisted. Needs to survive restarts, or the token rotates on every boot. |

Point `FIREBIN_API_URL` at the API service directly rather than through the web container. The web container's nginx proxies `/api` for the browser, and routing through it adds a hop for no benefit.

## Development

```sh
go build ./...
go vet ./...
go test -race ./...
```

Run it against a live instance:

```sh
FIREBIN_API_URL=http://localhost:8080 \
FIREBIN_ACCESS_TOKEN=fbin_pat_... \
FIREBIN_MCP_TOKEN=fbin_mcp_dev \
FIREBIN_MCP_DATA_DIR=./.local \
go run ./cmd/mcp
```

Releases are cut from Actions, Release, Run workflow. That computes the next `YY.M.revision` from the last tag, builds the image for amd64 and arm64, pushes it to GHCR, and tags the commit. No version string is committed; local builds report `YY.M.DEV`.

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
