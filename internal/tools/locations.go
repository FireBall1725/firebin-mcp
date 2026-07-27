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

// Location is a storage bin. The API returns a flat list with parent_id
// pointers; Path is assembled here so one row reads "Shop / Cabinet A /
// Drawer 3" and the model never has to walk the tree itself.
type Location struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	ParentID    string `json:"parent_id,omitempty"`
	Barcode     string `json:"barcode,omitempty"`
	Description string `json:"description,omitempty"`
}

// buildPaths resolves each location's full slash-separated path. A cycle in
// parent_id (which the API should never produce) stops the walk rather than
// hanging.
func buildPaths(locs []apiLocation) map[string]string {
	byID := make(map[string]apiLocation, len(locs))
	for _, l := range locs {
		byID[l.ID] = l
	}
	paths := make(map[string]string, len(locs))

	var resolve func(id string, depth int) string
	resolve = func(id string, depth int) string {
		if p, ok := paths[id]; ok {
			return p
		}
		l, ok := byID[id]
		if !ok {
			return ""
		}
		path := l.Name
		if l.ParentID != nil && depth < len(locs) {
			if parent := resolve(*l.ParentID, depth+1); parent != "" {
				path = parent + " / " + l.Name
			}
		}
		paths[id] = path
		return path
	}

	for _, l := range locs {
		resolve(l.ID, 0)
	}
	return paths
}

func toLocations(locs []apiLocation) []Location {
	paths := buildPaths(locs)
	out := make([]Location, 0, len(locs))
	for _, l := range locs {
		out = append(out, Location{
			ID:          l.ID,
			Name:        l.Name,
			Path:        paths[l.ID],
			ParentID:    deref(l.ParentID),
			Barcode:     deref(l.Barcode),
			Description: deref(l.Description),
		})
	}
	return out
}

// ─── list_locations ──────────────────────────────────────────────────────────

type listLocationsArgs struct{}

type listLocationsResult struct {
	Locations []Location `json:"locations"`
	Count     int        `json:"count"`
}

// AddListLocations wires the list_locations tool.
func AddListLocations(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_locations",
		Description: "List every storage location (bin, drawer, shelf, cabinet) in the inventory, each with its full path and scannable barcode. " +
			"Use this to turn a bin name a user mentions into the location id that adjust_stock, move_stock, and search filters need.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listLocationsArgs) (*mcp.CallToolResult, listLocationsResult, error) {
		locs, err := api.Get[[]apiLocation](ctx, client, "/locations")
		if err != nil {
			return nil, listLocationsResult{}, toolError(err)
		}
		out := toLocations(locs)
		return nil, listLocationsResult{Locations: out, Count: len(out)}, nil
	})
}

// ─── location_contents ───────────────────────────────────────────────────────

type locationContentsArgs struct {
	LocationID string `json:"location_id,omitempty" jsonschema:"Storage location id (a UUID). Provide this or barcode."`
	Barcode    string `json:"barcode,omitempty" jsonschema:"The bin's scanned barcode. Provide this or location_id."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum lots to list. Defaults to 100. The counts and total units always reflect the whole bin, not just the listed rows."`
}

type locationContentsResult struct {
	Location  Location    `json:"location"`
	Contents  []StockLine `json:"contents"`
	Count     int         `json:"count"`
	Units     float64     `json:"total_units"`
	Truncated bool        `json:"truncated,omitempty"`
	Note      string      `json:"note,omitempty"`
}

const defaultContentsLimit = 100

// AddLocationContents wires the location_contents tool.
func AddLocationContents(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "location_contents",
		Description: "List everything stored in one bin, identified either by its location id or by a scanned barcode. This is the 'what is in this drawer' lookup. " +
			"Each row carries a stock_item_id you can hand to move_stock.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args locationContentsArgs) (*mcp.CallToolResult, locationContentsResult, error) {
		id := strings.TrimSpace(args.LocationID)
		barcode := strings.TrimSpace(args.Barcode)
		if id == "" && barcode == "" {
			return nil, locationContentsResult{}, fmt.Errorf("provide either location_id or barcode")
		}

		var loc apiLocation
		var err error
		if id != "" {
			loc, err = api.Get[apiLocation](ctx, client, "/locations/"+url.PathEscape(id))
		} else {
			loc, err = api.Get[apiLocation](ctx, client, "/locations/scan?barcode="+url.QueryEscape(barcode))
		}
		if err != nil {
			return nil, locationContentsResult{}, toolError(err)
		}

		items, err := api.Get[[]apiStockItem](ctx, client, "/locations/"+url.PathEscape(loc.ID)+"/stock")
		if err != nil {
			return nil, locationContentsResult{}, toolError(err)
		}

		res := locationContentsResult{
			Location: Location{
				ID:          loc.ID,
				Name:        loc.Name,
				Path:        loc.Name,
				ParentID:    deref(loc.ParentID),
				Barcode:     deref(loc.Barcode),
				Description: deref(loc.Description),
			},
			Contents: make([]StockLine, 0, len(items)),
		}

		// The counts describe the whole bin even when the listing is capped,
		// so a truncated answer never understates what is in there.
		for _, it := range items {
			res.Units += it.Quantity
		}
		res.Count = len(items)

		limit := args.Limit
		if limit <= 0 {
			limit = defaultContentsLimit
		}
		page, total, truncated := capList(items, limit)
		res.Truncated = truncated
		if truncated {
			res.Note = fmt.Sprintf("Listing %d of %d lots in this bin; the count and total units cover all %d.", len(page), total, total)
		}

		for _, it := range page {
			line := toStockLine(it)
			// Inside a single bin the location is a given; the part is what
			// the caller actually needs to see.
			line.Location, line.LocationID = "", ""
			line.LotName = firstNonEmptyLot(line.LotName, it.PartName)
			res.Contents = append(res.Contents, line)
		}
		return nil, res, nil
	})
}

// firstNonEmptyLot prefers an explicit lot label and falls back to the part
// name, so a bin listing always names what the row is.
func firstNonEmptyLot(lot, partName string) string {
	if lot != "" {
		return lot
	}
	return partName
}

// ─── create_location ─────────────────────────────────────────────────────────

type locationRequest struct {
	ParentID    *string `json:"parent_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Barcode     *string `json:"barcode"`
}

type createLocationArgs struct {
	Name        string `json:"name" jsonschema:"Name of the new bin, drawer, shelf, or cabinet."`
	ParentID    string `json:"parent_id,omitempty" jsonschema:"Id (a UUID) of the location this sits inside. Omit for a top-level location."`
	Description string `json:"description,omitempty" jsonschema:"Optional description."`
	Barcode     string `json:"barcode,omitempty" jsonschema:"Optional scannable barcode to attach, so the bin can be looked up by scanning it."`
}

type createLocationResult struct {
	Location Location `json:"location"`
}

// AddCreateLocation wires the create_location tool.
func AddCreateLocation(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_location",
		Description: "Create a new storage location. Nest it under an existing one with parent_id to build up a hierarchy like Shop / Cabinet A / Drawer 3.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args createLocationArgs) (*mcp.CallToolResult, createLocationResult, error) {
		name := strings.TrimSpace(args.Name)
		if name == "" {
			return nil, createLocationResult{}, fmt.Errorf("name is required")
		}
		req := locationRequest{Name: name}
		if v := strings.TrimSpace(args.ParentID); v != "" {
			req.ParentID = &v
		}
		if v := strings.TrimSpace(args.Description); v != "" {
			req.Description = &v
		}
		if v := strings.TrimSpace(args.Barcode); v != "" {
			req.Barcode = &v
		}

		loc, err := api.Post[locationRequest, apiLocation](ctx, client, "/locations", req)
		if err != nil {
			return nil, createLocationResult{}, toolError(err)
		}
		return nil, createLocationResult{Location: Location{
			ID:          loc.ID,
			Name:        loc.Name,
			Path:        loc.Name,
			ParentID:    deref(loc.ParentID),
			Barcode:     deref(loc.Barcode),
			Description: deref(loc.Description),
		}}, nil
	})
}
