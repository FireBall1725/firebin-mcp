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

const defaultSearchLimit = 25

// ─── search_parts ────────────────────────────────────────────────────────────

type searchPartsArgs struct {
	Query           string `json:"query,omitempty" jsonschema:"Text to match against part name, keywords, IPN, and manufacturer part number. Substring match, so prefer one short distinctive term ('MOC3063' or 'LM317') over a full sentence. Omit to list everything."`
	Category        string `json:"category,omitempty" jsonschema:"Category id (a UUID) to restrict the search to. Use list_categories to resolve a category name to its id."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum rows to return. Defaults to 25, capped at 200."`
	IncludeVariants bool   `json:"include_variants,omitempty" jsonschema:"Include variant parts as their own rows. By default only top-level parts are returned and variants are counted in variant_count."`
}

type searchPartsResult struct {
	Parts     []PartSummary `json:"parts"`
	Total     int           `json:"total"`
	Truncated bool          `json:"truncated,omitempty"`
	Note      string        `json:"note,omitempty"`
}

// AddSearchParts wires the search_parts tool.
func AddSearchParts(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "search_parts",
		Description: "Search the electronics inventory for parts by name, keywords, internal part number (IPN), or manufacturer part number (MPN). " +
			"Matching is a plain case-insensitive substring test with no ranking or fuzzy matching, so a short distinctive term works far better than a phrase. " +
			"Returns compact summaries including current stock and the bin it lives in; call get_part with a returned id for specs, distributor pricing, and the per-bin breakdown.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args searchPartsArgs) (*mcp.CallToolResult, searchPartsResult, error) {
		limit := args.Limit
		switch {
		case limit <= 0:
			limit = defaultSearchLimit
		case limit > 200:
			limit = 200
		}

		q := url.Values{}
		if s := strings.TrimSpace(args.Query); s != "" {
			q.Set("search", s)
		}
		if c := strings.TrimSpace(args.Category); c != "" {
			q.Set("category", c)
		}
		if args.IncludeVariants {
			q.Set("top_level", "false")
		}

		parts, err := api.Get[[]apiPart](ctx, client, "/parts?"+q.Encode())
		if err != nil {
			return nil, searchPartsResult{}, toolError(err)
		}

		page, total, truncated := capList(parts, limit)
		res := searchPartsResult{Parts: toPartSummaries(page), Total: total, Truncated: truncated}
		if truncated {
			res.Note = fmt.Sprintf("Showing %d of %d matches. Narrow the query or raise limit.", len(page), total)
		}
		return nil, res, nil
	})
}

// ─── get_part ────────────────────────────────────────────────────────────────

type getPartArgs struct {
	ID string `json:"id" jsonschema:"The part id (a UUID), as returned by search_parts or scan_barcode."`
}

type getPartResult struct {
	Part PartDetail `json:"part"`
}

// AddGetPart wires the get_part tool.
func AddGetPart(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_part",
		Description: "Get one part in full: description, package, specification parameters, every manufacturer part number with its distributor SKUs and price breaks, " +
			"and the stock breakdown showing how many are in each bin. Use search_parts first to find the id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getPartArgs) (*mcp.CallToolResult, getPartResult, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return nil, getPartResult{}, fmt.Errorf("id is required")
		}
		part, err := api.Get[apiPart](ctx, client, "/parts/"+url.PathEscape(id))
		if err != nil {
			return nil, getPartResult{}, toolError(err)
		}
		// A part with no stock rows 404s on nothing — the endpoint returns an
		// empty list — but a transport failure here shouldn't lose the part we
		// already have, so a stock error degrades to "no breakdown".
		stock, err := api.Get[[]apiStockItem](ctx, client, "/parts/"+url.PathEscape(id)+"/stock")
		if err != nil {
			stock = nil
		}
		return nil, getPartResult{Part: toPartDetail(part, stock)}, nil
	})
}

// ─── update_part ─────────────────────────────────────────────────────────────

// partRequest mirrors firebin-api's handler-side request struct exactly. The
// API decodes with DisallowUnknownFields and treats PATCH as a full-object
// replacement, so this must be the whole field set and nothing more — echoing
// back a fetched part (which carries id, created_at, total_stock, variants…)
// is a 400.
type partRequest struct {
	CategoryID        *string        `json:"category_id"`
	VariantOf         *string        `json:"variant_of"`
	Name              string         `json:"name"`
	Description       *string        `json:"description"`
	IPN               *string        `json:"ipn"`
	Package           *string        `json:"package"`
	Keywords          *string        `json:"keywords"`
	Barcode           *string        `json:"barcode"`
	ImagePath         *string        `json:"image_path"`
	IsTemplate        bool           `json:"is_template"`
	IsAssembly        bool           `json:"is_assembly"`
	MinimumStock      float64        `json:"minimum_stock"`
	DefaultLocationID *string        `json:"default_location_id"`
	Parameters        []paramRequest `json:"parameters"`
}

type paramRequest struct {
	Name  string  `json:"name"`
	Units *string `json:"units"`
	Value string  `json:"value"`
}

// fromPart builds the replacement request out of a fetched part, so an update
// that touches one field preserves everything else.
func fromPart(p apiPart) partRequest {
	req := partRequest{
		CategoryID:        p.CategoryID,
		VariantOf:         p.VariantOf,
		Name:              p.Name,
		Description:       p.Description,
		IPN:               p.IPN,
		Package:           p.Package,
		Keywords:          p.Keywords,
		Barcode:           p.Barcode,
		ImagePath:         p.ImagePath,
		IsTemplate:        p.IsTemplate,
		IsAssembly:        p.IsAssembly,
		MinimumStock:      p.MinimumStock,
		DefaultLocationID: p.DefaultLocationID,
		Parameters:        []paramRequest{},
	}
	for _, param := range p.Parameters {
		req.Parameters = append(req.Parameters, paramRequest{
			Name:  param.TemplateName,
			Units: param.Units,
			Value: param.Value,
		})
	}
	return req
}

type updatePartArgs struct {
	ID                string   `json:"id" jsonschema:"The part id (a UUID) to update."`
	Name              *string  `json:"name,omitempty" jsonschema:"New part name."`
	Description       *string  `json:"description,omitempty" jsonschema:"New description."`
	IPN               *string  `json:"ipn,omitempty" jsonschema:"New internal part number."`
	Package           *string  `json:"package,omitempty" jsonschema:"New package or footprint, e.g. 0603, SOT-23, DIP-8."`
	Keywords          *string  `json:"keywords,omitempty" jsonschema:"New search keywords, free text."`
	MinimumStock      *float64 `json:"minimum_stock,omitempty" jsonschema:"Reorder threshold. The part shows up in low_stock once total stock drops below this."`
	CategoryID        *string  `json:"category_id,omitempty" jsonschema:"New category id (a UUID). Use list_categories to resolve a name."`
	DefaultLocationID *string  `json:"default_location_id,omitempty" jsonschema:"New default storage location id (a UUID)."`
}

type updatePartResult struct {
	Part PartDetail `json:"part"`
}

// AddUpdatePart wires the update_part tool.
func AddUpdatePart(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "update_part",
		Description: "Change fields on an existing part. Only the fields you pass are altered; everything else, including specification parameters and manufacturer part numbers, is preserved. " +
			"Use this to rename a part, set its package, or set the minimum_stock threshold that drives low_stock. To change quantities use adjust_stock instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args updatePartArgs) (*mcp.CallToolResult, updatePartResult, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return nil, updatePartResult{}, fmt.Errorf("id is required")
		}

		current, err := api.Get[apiPart](ctx, client, "/parts/"+url.PathEscape(id))
		if err != nil {
			return nil, updatePartResult{}, toolError(err)
		}

		req := fromPart(current)
		if args.Name != nil {
			if strings.TrimSpace(*args.Name) == "" {
				return nil, updatePartResult{}, fmt.Errorf("name cannot be blank")
			}
			req.Name = *args.Name
		}
		if args.Description != nil {
			req.Description = args.Description
		}
		if args.IPN != nil {
			req.IPN = args.IPN
		}
		if args.Package != nil {
			req.Package = args.Package
		}
		if args.Keywords != nil {
			req.Keywords = args.Keywords
		}
		if args.MinimumStock != nil {
			req.MinimumStock = *args.MinimumStock
		}
		if args.CategoryID != nil {
			req.CategoryID = args.CategoryID
		}
		if args.DefaultLocationID != nil {
			req.DefaultLocationID = args.DefaultLocationID
		}

		if _, err := api.Patch[partRequest, apiPart](ctx, client, "/parts/"+url.PathEscape(id), req); err != nil {
			return nil, updatePartResult{}, toolError(err)
		}

		// Re-read rather than returning the PATCH response. That response
		// omits the manufacturer parts and stock, which reads as though the
		// update wiped them; it did not, but the model has no way to know
		// that from an empty array.
		final, err := api.Get[apiPart](ctx, client, "/parts/"+url.PathEscape(id))
		if err != nil {
			return nil, updatePartResult{}, toolError(err)
		}
		stock, _ := api.Get[[]apiStockItem](ctx, client, "/parts/"+url.PathEscape(id)+"/stock")
		return nil, updatePartResult{Part: toPartDetail(final, stock)}, nil
	})
}
