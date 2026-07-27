// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import (
	"context"

	"github.com/firelabsca/firebin-mcp/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ─── list_categories ─────────────────────────────────────────────────────────

// Category is a part category with the count of parts filed directly under it.
type Category struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	ParentID    string `json:"parent_id,omitempty"`
	PartCount   int    `json:"part_count"`
	Description string `json:"description,omitempty"`
}

type listCategoriesArgs struct{}

type listCategoriesResult struct {
	Categories []Category `json:"categories"`
	Count      int        `json:"count"`
}

// AddListCategories wires the list_categories tool.
func AddListCategories(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_categories",
		Description: "List the part categories with how many parts are filed in each. " +
			"Use this to turn a category name into the id that search_parts and update_part take.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listCategoriesArgs) (*mcp.CallToolResult, listCategoriesResult, error) {
		cats, err := api.Get[[]apiCategory](ctx, client, "/categories")
		if err != nil {
			return nil, listCategoriesResult{}, toolError(err)
		}
		paths := buildCategoryPaths(cats)
		out := make([]Category, 0, len(cats))
		for _, c := range cats {
			out = append(out, Category{
				ID:          c.ID,
				Name:        c.Name,
				Path:        paths[c.ID],
				ParentID:    deref(c.ParentID),
				PartCount:   c.PartCount,
				Description: deref(c.Description),
			})
		}
		return nil, listCategoriesResult{Categories: out, Count: len(out)}, nil
	})
}

// buildCategoryPaths mirrors buildPaths for the category tree.
func buildCategoryPaths(cats []apiCategory) map[string]string {
	byID := make(map[string]apiCategory, len(cats))
	for _, c := range cats {
		byID[c.ID] = c
	}
	paths := make(map[string]string, len(cats))

	var resolve func(id string, depth int) string
	resolve = func(id string, depth int) string {
		if p, ok := paths[id]; ok {
			return p
		}
		c, ok := byID[id]
		if !ok {
			return ""
		}
		path := c.Name
		if c.ParentID != nil && depth < len(cats) {
			if parent := resolve(*c.ParentID, depth+1); parent != "" {
				path = parent + " / " + c.Name
			}
		}
		paths[id] = path
		return path
	}

	for _, c := range cats {
		resolve(c.ID, 0)
	}
	return paths
}

// ─── inventory_stats ─────────────────────────────────────────────────────────

type inventoryStatsArgs struct{}

type inventoryStatsResult struct {
	PartsCount     int     `json:"parts_count"`
	VariantsCount  int     `json:"variants_count"`
	LocationsCount int     `json:"locations_count"`
	LowStockCount  int     `json:"low_stock_count"`
	TotalUnits     float64 `json:"total_units"`
	InventoryValue float64 `json:"inventory_value"`
}

// AddInventoryStats wires the inventory_stats tool.
func AddInventoryStats(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "inventory_stats",
		Description: "Summary numbers for the whole inventory: how many distinct parts and variants, how many storage locations, how many parts are below their minimum, total units on hand, and total inventory value.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ inventoryStatsArgs) (*mcp.CallToolResult, inventoryStatsResult, error) {
		s, err := api.Get[apiStats](ctx, client, "/stats")
		if err != nil {
			return nil, inventoryStatsResult{}, toolError(err)
		}
		return nil, inventoryStatsResult(s), nil
	})
}
