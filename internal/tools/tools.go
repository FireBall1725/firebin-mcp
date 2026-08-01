// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

// Package tools exposes the MCP tool surface of the FireBin MCP server.
// Each tool is a thin translator: validate the arguments, call the public
// FireBin REST API through the shared client, and project the response into
// a shape that is compact and useful inside a conversation.
//
// v1 catalogue:
//
//	reads:  search_parts, get_part, list_locations, location_contents,
//	        low_stock, inventory_stats, list_categories, stock_history,
//	        recent_activity, lookup_mpn, list_projects, get_project,
//	        pick_list
//	writes: scan_barcode, adjust_stock, move_stock, add_part_by_mpn,
//	        update_part, create_location
//
// Two constraints shape everything here.
//
// First, the API paginates nothing: GET /parts returns the entire inventory
// in one response, and a Part carries its variants, parameters, manufacturer
// parts, supplier SKUs, price breaks and alternates inline. So every list
// tool caps its output and says so, and every read projects the raw row into
// a trimmed struct rather than passing it through.
//
// Second, authority comes from the role of the account that minted
// FIREBIN_ACCESS_TOKEN, not from the token's scopes — firebin-api stores PAT
// scopes but no middleware reads them. A token minted by a viewer makes every
// write tool here fail with a clean 403; one minted by an admin would let a
// future tool reach admin routes. Mint it from a dedicated member account.
//
// Deliberately not exposed in v1:
//
//   - Labels, printing, and previews. Those endpoints return PDF bytes, and
//     the tape printer is driven over WebUSB from the browser.
//   - Part images and project assets: binary payloads with no conversational
//     use.
//   - Stock lot split, merge, relocate, and lot-adjust. Cutting a reel into a
//     barcoded mini spool is a physical bench task done with a scanner in
//     hand, not something to drive blind through chat.
//   - Bill-of-materials and board upload. Multipart file ingest.
//   - Tasks and the job queue.
//   - Everything behind RequireAdmin: users, settings, enrichment
//     configuration, backup export and import, empty-lot cleanup.
//   - Every DELETE. This server cannot destroy a part, a location, a project,
//     or a stock lot.
package tools

import (
	"github.com/firelabsca/firebin-mcp/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterAll wires every tool this package owns onto the given MCP server,
// so cmd/mcp stays a single call as the catalogue grows.
func RegisterAll(srv *mcp.Server, client *api.Client) {
	// Parts
	AddSearchParts(srv, client)
	AddSearchPartsBySpec(srv, client)
	AddGetPart(srv, client)
	AddUpdatePart(srv, client)

	// Stock
	AddLowStock(srv, client)
	AddStockHistory(srv, client)
	AddRecentActivity(srv, client)
	AddAdjustStock(srv, client)
	AddMoveStock(srv, client)

	// Locations
	AddListLocations(srv, client)
	AddLocationContents(srv, client)
	AddCreateLocation(srv, client)

	// Catalog
	AddListCategories(srv, client)
	AddInventoryStats(srv, client)

	// Enrichment
	AddLookupMPN(srv, client)
	AddAddPartByMPN(srv, client)

	// Scanning
	AddScanBarcode(srv, client)

	// Projects
	AddListProjects(srv, client)
	AddGetProject(srv, client)
	AddGetBoard(srv, client)
	AddPickList(srv, client)
}
