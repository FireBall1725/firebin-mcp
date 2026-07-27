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

// ─── Enrichment wire shapes ──────────────────────────────────────────────────

type apiEnrichResponse struct {
	Found  bool           `json:"found"`
	Cached bool           `json:"cached"`
	Part   apiEnrichedPart `json:"part"`
}

type apiEnrichedPart struct {
	MPN          string               `json:"mpn"`
	Name         string               `json:"name"` // already cleaned server-side
	Description  string               `json:"description"`
	Manufacturer string               `json:"manufacturer"`
	Category     string               `json:"category"`
	Package      string               `json:"package"`
	DatasheetURL string               `json:"datasheet_url"`
	ImageURL     string               `json:"image_url"`
	Parameters   []apiEnrichedParam   `json:"parameters"`
	Suppliers    []apiEnrichedSupplier `json:"suppliers"`
	Alternatives []apiAlternative     `json:"alternatives"`
	Source       string               `json:"source"`
}

type apiEnrichedParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Units string `json:"units"`
}

// apiEnrichedSupplier mirrors models.EnrichedSupplier. Note the price-break
// field is `prices` here, while the stored SupplierPart on a saved part calls
// the same thing `pricing`; the two shapes are not interchangeable.
type apiEnrichedSupplier struct {
	Name      string          `json:"name"`
	SKU       string          `json:"sku"`
	URL       string          `json:"url"`
	Packaging string          `json:"packaging"`
	Prices    []apiPriceBreak `json:"prices"`
}

// EnrichedPart is what lookup_mpn hands back: the distributor's view of a
// part, with nothing written to the inventory.
type EnrichedPart struct {
	MPN          string           `json:"mpn"`
	SuggestedName string          `json:"suggested_name,omitempty"`
	Manufacturer string           `json:"manufacturer,omitempty"`
	Description  string           `json:"description,omitempty"`
	Category     string           `json:"category,omitempty"`
	Package      string           `json:"package,omitempty"`
	Datasheet    string           `json:"datasheet,omitempty"`
	Parameters   []Parameter      `json:"parameters,omitempty"`
	Suppliers    []SupplierOffer  `json:"suppliers,omitempty"`
	Alternatives []apiAlternative `json:"alternatives,omitempty"`
	Source       string           `json:"source,omitempty"`
	Cached       bool             `json:"cached,omitempty"`
}

func toEnrichedPart(r apiEnrichResponse) EnrichedPart {
	p := r.Part
	out := EnrichedPart{
		MPN:           p.MPN,
		SuggestedName: p.Name,
		Manufacturer:  p.Manufacturer,
		Description:   p.Description,
		Category:      p.Category,
		Package:       p.Package,
		Datasheet:     p.DatasheetURL,
		Alternatives:  p.Alternatives,
		Source:        p.Source,
		Cached:        r.Cached,
	}
	for _, param := range p.Parameters {
		out.Parameters = append(out.Parameters, Parameter{Name: param.Name, Value: param.Value, Units: param.Units})
	}
	for _, s := range p.Suppliers {
		out.Suppliers = append(out.Suppliers, SupplierOffer{
			Supplier:  s.Name,
			SKU:       s.SKU,
			Packaging: s.Packaging,
			Pricing:   formatPriceBreaks(s.Prices),
			URL:       s.URL,
		})
	}
	return out
}

// enrichByMPN is the shared lookup used by both lookup_mpn and
// add_part_by_mpn.
func enrichByMPN(ctx context.Context, client *api.Client, mpn string, providers []string, refresh bool) (apiEnrichResponse, error) {
	q := url.Values{}
	q.Set("mpn", mpn)
	if len(providers) > 0 {
		q.Set("providers", strings.Join(providers, ","))
	}
	if refresh {
		q.Set("refresh", "true")
	}
	return api.Get[apiEnrichResponse](ctx, client, "/enrich?"+q.Encode())
}

// ─── lookup_mpn ──────────────────────────────────────────────────────────────

type lookupMPNArgs struct {
	MPN       string   `json:"mpn" jsonschema:"The manufacturer part number to look up, e.g. MOC3063S or GRM188R71C104KA01D."`
	Providers []string `json:"providers,omitempty" jsonschema:"Restrict the lookup to specific enrichment providers, e.g. digikey or nexar. Omit to query every configured provider."`
	Refresh   bool     `json:"refresh,omitempty" jsonschema:"Skip the cache and re-query the providers. Use only when you need current pricing."`
}

type lookupMPNResult struct {
	Found          bool          `json:"found"`
	Part           *EnrichedPart `json:"part,omitempty"`
	ExistingPartID string        `json:"existing_part_id,omitempty"`
	Note           string        `json:"note,omitempty"`
}

// AddLookupMPN wires the lookup_mpn tool.
func AddLookupMPN(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "lookup_mpn",
		Description: "Look up a manufacturer part number with the configured distributor providers (Digi-Key, Nexar) and return its specifications, package, datasheet, and current distributor pricing. " +
			"This is read-only: nothing is written to the inventory. It also reports whether a part with that MPN already exists. Use add_part_by_mpn when you actually want to create the part.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args lookupMPNArgs) (*mcp.CallToolResult, lookupMPNResult, error) {
		mpn := strings.TrimSpace(args.MPN)
		if mpn == "" {
			return nil, lookupMPNResult{}, fmt.Errorf("mpn is required")
		}

		resp, err := enrichByMPN(ctx, client, mpn, args.Providers, args.Refresh)
		if err != nil {
			return nil, lookupMPNResult{}, toolError(err)
		}
		res := lookupMPNResult{Found: resp.Found}
		if !resp.Found {
			res.Note = fmt.Sprintf("No distributor data found for %q. Check the part number, or the enrichment providers may not be configured on this instance.", mpn)
			return nil, res, nil
		}
		p := toEnrichedPart(resp)
		res.Part = &p

		// Tell the caller up front whether creating this would be a duplicate.
		// A search failure here is not worth failing the lookup over.
		if existing, err := findPartByMPN(ctx, client, mpn); err == nil && existing != "" {
			res.ExistingPartID = existing
			res.Note = "A part with this MPN already exists in the inventory; use get_part on existing_part_id rather than creating a duplicate."
		}
		return nil, res, nil
	})
}

// findPartByMPN returns the id of an existing part carrying this MPN, or "".
// The parts search already covers manufacturer part numbers, so this is one
// call rather than a scan.
func findPartByMPN(ctx context.Context, client *api.Client, mpn string) (string, error) {
	q := url.Values{}
	q.Set("search", mpn)
	q.Set("top_level", "false")
	parts, err := api.Get[[]apiPart](ctx, client, "/parts?"+q.Encode())
	if err != nil {
		return "", err
	}
	for _, p := range parts {
		if strings.EqualFold(deref(p.PrimaryMPN), mpn) {
			return p.ID, nil
		}
		for _, mp := range p.ManufacturerParts {
			if strings.EqualFold(mp.MPN, mpn) {
				return p.ID, nil
			}
		}
	}
	return "", nil
}

// ─── add_part_by_mpn ─────────────────────────────────────────────────────────

type manufacturerPartRequest struct {
	Manufacturer string  `json:"manufacturer"`
	MPN          string  `json:"mpn"`
	DatasheetURL *string `json:"datasheet_url"`
}

type enrichPartRequest struct {
	Providers []string `json:"providers"`
}

type addPartByMPNArgs struct {
	MPN        string   `json:"mpn" jsonschema:"The manufacturer part number to create a part from."`
	Quantity   float64  `json:"quantity,omitempty" jsonschema:"Units to book into stock straight away. Omit or pass 0 to create the part with no stock."`
	LocationID string   `json:"location_id,omitempty" jsonschema:"Storage location id (a UUID) for the initial stock and the part's default bin. Use list_locations to resolve a name."`
	CategoryID string   `json:"category_id,omitempty" jsonschema:"Category id (a UUID) to file the part under. Use list_categories to resolve a name."`
	Providers  []string `json:"providers,omitempty" jsonschema:"Restrict enrichment to specific providers, e.g. digikey or nexar."`
}

type addPartByMPNResult struct {
	Part     PartDetail `json:"part"`
	Created  bool       `json:"created"`
	Warnings []string   `json:"warnings,omitempty"`
	Note     string     `json:"note,omitempty"`
}

// AddAddPartByMPN wires the add_part_by_mpn tool.
//
// This is the one tool that chains several API calls, and the order is forced
// by the backend: POST /parts/{id}/enrich refuses a part that has no
// manufacturer part attached yet ("part has no MPN to look up"), so the MPN
// has to be attached between creating the part and enriching it.
func AddAddPartByMPN(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "add_part_by_mpn",
		Description: "Create a new inventory part from a manufacturer part number, filling in its name, description, package, specifications, datasheet, and distributor SKUs with pricing from the enrichment providers. " +
			"Optionally books an opening stock quantity into a bin at the same time. Check lookup_mpn first if you want to see the data before committing to it; " +
			"this tool refuses to run if the MPN returns no distributor data, and warns rather than duplicating if the part already exists.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args addPartByMPNArgs) (*mcp.CallToolResult, addPartByMPNResult, error) {
		mpn := strings.TrimSpace(args.MPN)
		if mpn == "" {
			return nil, addPartByMPNResult{}, fmt.Errorf("mpn is required")
		}

		// Refuse to create a second part for an MPN already on file. Silently
		// duplicating is worse than doing nothing.
		if existing, err := findPartByMPN(ctx, client, mpn); err == nil && existing != "" {
			part, getErr := api.Get[apiPart](ctx, client, "/parts/"+url.PathEscape(existing))
			if getErr != nil {
				return nil, addPartByMPNResult{}, toolError(getErr)
			}
			stock, _ := api.Get[[]apiStockItem](ctx, client, "/parts/"+url.PathEscape(existing)+"/stock")
			return nil, addPartByMPNResult{
				Part:    toPartDetail(part, stock),
				Created: false,
				Note:    "A part with this MPN already exists, so nothing was created. Use adjust_stock on this part to book in more units.",
			}, nil
		}

		// 1. Look up the MPN first, so a bad part number fails before anything
		//    is written and no orphan part is left behind.
		enriched, err := enrichByMPN(ctx, client, mpn, args.Providers, false)
		if err != nil {
			return nil, addPartByMPNResult{}, toolError(err)
		}
		if !enriched.Found {
			return nil, addPartByMPNResult{}, fmt.Errorf(
				"no distributor data found for %q, so no part was created. Check the part number, or check that an enrichment provider is configured on this FireBin instance", mpn)
		}

		var warnings []string

		// 2. Create the part. The API already derived a clean display name.
		create := partRequest{Name: enriched.Part.Name, Parameters: []paramRequest{}}
		if create.Name == "" {
			create.Name = mpn
		}
		if v := strings.TrimSpace(args.CategoryID); v != "" {
			create.CategoryID = &v
		}
		if v := strings.TrimSpace(args.LocationID); v != "" {
			create.DefaultLocationID = &v
		}
		part, err := api.Post[partRequest, apiPart](ctx, client, "/parts", create)
		if err != nil {
			return nil, addPartByMPNResult{}, toolError(err)
		}
		partPath := "/parts/" + url.PathEscape(part.ID)

		// 3. Attach the MPN. Required before step 4 will do anything.
		mfr := enriched.Part.Manufacturer
		mpReq := manufacturerPartRequest{Manufacturer: mfr, MPN: mpn}
		if ds := enriched.Part.DatasheetURL; ds != "" {
			mpReq.DatasheetURL = &ds
		}
		if _, err := api.Post[manufacturerPartRequest, map[string]any](ctx, client, partPath+"/manufacturer-parts", mpReq); err != nil {
			warnings = append(warnings, "could not attach the manufacturer part number: "+err.Error())
		} else {
			// 4. Apply the enrichment server-side — the same path the web UI
			//    uses, so a part added here is indistinguishable from one
			//    added in the browser.
			if _, err := api.Post[enrichPartRequest, map[string]any](ctx, client, partPath+"/enrich", enrichPartRequest{Providers: args.Providers}); err != nil {
				warnings = append(warnings, "the part was created but enrichment did not apply: "+err.Error())
			}
		}

		// 5. Book opening stock.
		if args.Quantity > 0 {
			adj := adjustRequest{Kind: "add", Quantity: args.Quantity}
			if v := strings.TrimSpace(args.LocationID); v != "" {
				adj.LocationID = &v
			}
			if _, err := api.Post[adjustRequest, apiStockItem](ctx, client, partPath+"/stock/adjust", adj); err != nil {
				warnings = append(warnings, "the part was created but the opening stock was not booked: "+err.Error())
			}
		}

		// 6. Re-read so the caller sees what actually landed.
		final, err := api.Get[apiPart](ctx, client, partPath)
		if err != nil {
			return nil, addPartByMPNResult{}, toolError(err)
		}
		stock, _ := api.Get[[]apiStockItem](ctx, client, partPath+"/stock")

		return nil, addPartByMPNResult{
			Part:     toPartDetail(final, stock),
			Created:  true,
			Warnings: warnings,
		}, nil
	})
}
