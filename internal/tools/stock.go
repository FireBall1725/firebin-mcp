// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/firelabsca/firebin-mcp/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultHistoryLimit = 25

// ─── Movement projection ─────────────────────────────────────────────────────

// Movement is one line of stock history.
//
// A move between bins is written to the ledger as a pair of rows (a subtract
// from the source, an add to the destination), but firebin-api already folds
// that pair into one row before serving it — see collapseMoves in the API's
// internal/repository/stock.go, applied by both Recent and ListTransactions.
// On a collapsed move row, delta is the magnitude that landed at the
// destination and both location names are filled in, so there is nothing to
// merge on this side.
type Movement struct {
	Kind      string  `json:"kind"`
	Part      string  `json:"part,omitempty"`
	PartID    string  `json:"part_id,omitempty"`
	Delta     float64 `json:"delta"`
	Resulting float64 `json:"resulting_quantity,omitempty"`
	From      string  `json:"from,omitempty"`
	To        string  `json:"to,omitempty"`
	Note      string  `json:"note,omitempty"`
	At        string  `json:"at"`
}

func toMovements(in []apiStockTxn) []Movement {
	out := make([]Movement, 0, len(in))
	for _, t := range in {
		out = append(out, Movement{
			Kind:      t.Kind,
			Part:      deref(t.PartName),
			PartID:    deref(t.PartID),
			Delta:     t.Delta,
			Resulting: t.ResultingQuantity,
			From:      deref(t.FromLocationName),
			To:        deref(t.ToLocationName),
			Note:      deref(t.Note),
			At:        t.CreatedAt,
		})
	}
	return out
}

// ─── low_stock ───────────────────────────────────────────────────────────────

type lowStockArgs struct{}

type lowStockRow struct {
	PartSummary
	Short float64 `json:"short"`
}

type lowStockResult struct {
	Parts []lowStockRow `json:"parts"`
	Count int           `json:"count"`
}

// AddLowStock wires the low_stock tool.
func AddLowStock(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "low_stock",
		Description: "List every part whose total stock has fallen to or below its configured minimum, with how many units short it is. " +
			"This is the reorder list. A part only appears here if someone set a minimum_stock on it; use update_part to set that threshold.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ lowStockArgs) (*mcp.CallToolResult, lowStockResult, error) {
		parts, err := api.Get[[]apiPart](ctx, client, "/parts/low-stock")
		if err != nil {
			return nil, lowStockResult{}, toolError(err)
		}
		rows := make([]lowStockRow, 0, len(parts))
		for _, p := range parts {
			rows = append(rows, lowStockRow{
				PartSummary: toPartSummary(p),
				Short:       p.MinimumStock - p.TotalStock,
			})
		}
		return nil, lowStockResult{Parts: rows, Count: len(rows)}, nil
	})
}

// ─── stock_history ───────────────────────────────────────────────────────────

type stockHistoryArgs struct {
	PartID string `json:"part_id" jsonschema:"The part id (a UUID) whose stock movements to list."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum movements to return, newest first. Defaults to 25."`
}

type movementsResult struct {
	Movements []Movement `json:"movements"`
	Total     int        `json:"total"`
	Truncated bool       `json:"truncated,omitempty"`
}

// AddStockHistory wires the stock_history tool.
func AddStockHistory(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "stock_history",
		Description: "List the stock movements for one part, newest first: what was added, removed, counted, or moved between bins, when, and with what note. " +
			"Use this to answer questions like when a part was last restocked or where it came from.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args stockHistoryArgs) (*mcp.CallToolResult, movementsResult, error) {
		id := strings.TrimSpace(args.PartID)
		if id == "" {
			return nil, movementsResult{}, fmt.Errorf("part_id is required")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = defaultHistoryLimit
		}
		txns, err := api.Get[[]apiStockTxn](ctx, client, "/parts/"+url.PathEscape(id)+"/stock/history")
		if err != nil {
			return nil, movementsResult{}, toolError(err)
		}
		// The API itself caps this at the 100 most recent movements.
		page, total, truncated := capList(toMovements(txns), limit)
		return nil, movementsResult{Movements: page, Total: total, Truncated: truncated}, nil
	})
}

// ─── recent_activity ─────────────────────────────────────────────────────────

type recentActivityArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum movements to return, newest first. Defaults to 25."`
}

// AddRecentActivity wires the recent_activity tool.
func AddRecentActivity(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "recent_activity",
		Description: "List the most recent stock movements across the whole inventory, newest first. Answers 'what changed lately' and 'what did I book in today'.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args recentActivityArgs) (*mcp.CallToolResult, movementsResult, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = defaultHistoryLimit
		}
		txns, err := api.Get[[]apiStockTxn](ctx, client, "/stock/recent")
		if err != nil {
			return nil, movementsResult{}, toolError(err)
		}
		// The API itself serves only the 20 most recent movements here, so
		// this cap only ever bites when the caller asks for fewer.
		page, total, truncated := capList(toMovements(txns), limit)
		return nil, movementsResult{Movements: page, Total: total, Truncated: truncated}, nil
	})
}

// ─── adjust_stock ────────────────────────────────────────────────────────────

type adjustRequest struct {
	LocationID     *string `json:"location_id"`
	SupplierPartID *string `json:"supplier_part_id"`
	Kind           string  `json:"kind"`
	Quantity       float64 `json:"quantity"`
	Note           *string `json:"note"`
}

type adjustStockArgs struct {
	PartID     string  `json:"part_id" jsonschema:"The part id (a UUID) to adjust."`
	Kind       string  `json:"kind" jsonschema:"add to book units in, remove to consume units, count to set the on-hand quantity to an observed physical count, adjust to apply a signed correction."`
	Quantity   float64 `json:"quantity" jsonschema:"Number of units. For add and remove this is how many; for count it is the new on-hand total."`
	LocationID string  `json:"location_id,omitempty" jsonschema:"Storage location id (a UUID) to apply this to. Defaults to the part's existing lot when omitted. Use list_locations to resolve a bin name."`
	Note       string  `json:"note,omitempty" jsonschema:"Free-text note recorded on the movement, e.g. an order number or what the parts were used for."`
}

type adjustStockResult struct {
	StockItem StockLine `json:"stock_item"`
}

var validAdjustKinds = map[string]bool{"add": true, "remove": true, "count": true, "adjust": true}

// AddAdjustStock wires the adjust_stock tool.
func AddAdjustStock(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "adjust_stock",
		Description: "Change how many of a part are on hand, recording the movement in its history. " +
			"Use kind 'add' when parts arrive, 'remove' when they are consumed, 'count' to reconcile against a physical count (quantity becomes the new total), " +
			"and 'adjust' for a signed correction. To relocate existing units between bins use move_stock, which preserves the count.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args adjustStockArgs) (*mcp.CallToolResult, adjustStockResult, error) {
		id := strings.TrimSpace(args.PartID)
		if id == "" {
			return nil, adjustStockResult{}, fmt.Errorf("part_id is required")
		}
		kind := strings.ToLower(strings.TrimSpace(args.Kind))
		if !validAdjustKinds[kind] {
			return nil, adjustStockResult{}, fmt.Errorf("kind must be one of add, remove, count, adjust (got %q)", args.Kind)
		}

		req := adjustRequest{Kind: kind, Quantity: args.Quantity}
		if loc := strings.TrimSpace(args.LocationID); loc != "" {
			req.LocationID = &loc
		}
		if note := strings.TrimSpace(args.Note); note != "" {
			req.Note = &note
		}

		item, err := api.Post[adjustRequest, apiStockItem](ctx, client, "/parts/"+url.PathEscape(id)+"/stock/adjust", req)
		if err != nil {
			return nil, adjustStockResult{}, toolError(err)
		}
		return nil, adjustStockResult{StockItem: toStockLine(item)}, nil
	})
}

// ─── move_stock ──────────────────────────────────────────────────────────────

type moveRequest struct {
	StockItemID  string  `json:"stock_item_id"`
	ToLocationID *string `json:"to_location_id"`
	Quantity     float64 `json:"quantity"`
	Note         *string `json:"note"`
}

type moveStockArgs struct {
	StockItemID  string  `json:"stock_item_id" jsonschema:"The stock lot id (a UUID) to move. get_part lists these under stock as stock_item_id; location_contents and scan_barcode also return them."`
	ToLocationID string  `json:"to_location_id" jsonschema:"Destination storage location id (a UUID). Use list_locations to resolve a bin name."`
	Quantity     float64 `json:"quantity" jsonschema:"How many units to move. Move the whole lot by passing its full quantity."`
	Note         string  `json:"note,omitempty" jsonschema:"Free-text note recorded on the movement."`
}

type moveStockResult struct {
	Moved bool `json:"moved"`
}

// AddMoveStock wires the move_stock tool.
func AddMoveStock(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "move_stock",
		Description: "Relocate units of a stock lot from one bin to another. The total on-hand count is unchanged; only where the parts live changes. " +
			"To change the count itself use adjust_stock.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args moveStockArgs) (*mcp.CallToolResult, moveStockResult, error) {
		item := strings.TrimSpace(args.StockItemID)
		if item == "" {
			return nil, moveStockResult{}, fmt.Errorf("stock_item_id is required")
		}
		dest := strings.TrimSpace(args.ToLocationID)
		if dest == "" {
			return nil, moveStockResult{}, fmt.Errorf("to_location_id is required")
		}
		if args.Quantity <= 0 {
			return nil, moveStockResult{}, fmt.Errorf("quantity must be greater than zero")
		}

		req := moveRequest{StockItemID: item, ToLocationID: &dest, Quantity: args.Quantity}
		if note := strings.TrimSpace(args.Note); note != "" {
			req.Note = &note
		}
		if _, err := api.Post[moveRequest, map[string]any](ctx, client, "/stock/move", req); err != nil {
			return nil, moveStockResult{}, toolError(err)
		}
		return nil, moveStockResult{Moved: true}, nil
	})
}
