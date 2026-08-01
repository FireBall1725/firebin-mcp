// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/firelabsca/firebin-mcp/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ─── search_parts_by_spec ────────────────────────────────────────────────────

type specSearchArgs struct {
	Value     string `json:"value,omitempty" jsonschema:"A parameter value with or without a unit: '220 ohm', '4.7uF', '100nF', 'X7R', or a bare '220'. A value with a unit compares as a real quantity, so '220 ohm' never matches 220 pF and '100 ohm' never matches 100 kilohm. A bare number matches the magnitude printed on the part whatever its unit."`
	Package   string `json:"package,omitempty" jsonschema:"Package, matched as a substring, so '0603' finds '0603 (1608 Metric)'. This is a separate filter from value: an 0603 220 ohm is package '0603' and value '220 ohm', not one search string."`
	Parameter string `json:"parameter,omitempty" jsonschema:"Restrict the value filter to one named parameter, such as 'Resistance' or 'Capacitance'. Omit to match the value against any parameter."`
	Query     string `json:"query,omitempty" jsonschema:"Free text over name, keywords, IPN and manufacturer part number, combined with the filters above."`
	Category  string `json:"category,omitempty" jsonschema:"Category id (a UUID) to restrict the search to."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum rows to return. Defaults to 25, capped at 200."`
}

// SpecMatch is a part that matched, with the parameters that made it match.
type SpecMatch struct {
	PartSummary
	// Matched says why this part came back, so a caller does not need a second
	// request per candidate to find out.
	Matched []MatchedParameter `json:"matched,omitempty"`
	// ReferenceOnly marks a part recorded but not owned. Without it such a part
	// reads as "in stock: 0", which is true and misleading: that is sold out,
	// not never stocked.
	ReferenceOnly bool   `json:"reference_only,omitempty"`
	Note          string `json:"note,omitempty"`
}

type MatchedParameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type specSearchResult struct {
	Parts []SpecMatch `json:"parts"`
	Total int         `json:"total"`
	Note  string      `json:"note,omitempty"`
}

type apiSpecMatch struct {
	apiPart
	MatchedParameters []apiPartParam `json:"matched_parameters"`
}

// AddSearchPartsBySpec wires the search_parts_by_spec tool.
func AddSearchPartsBySpec(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "search_parts_by_spec",
		Description: "Search the inventory by what a part is rather than what it is called: package and parameter value, with unit-aware matching. " +
			"Use this for any question of the form 'do I have an 0603 220 ohm' or 'what 4.7uF capacitors do I have'. " +
			"search_parts cannot answer those: it is a substring match on the name, and the package lives in its own column while the value lives in the parameters, so neither is in the text it searches. " +
			"An empty result means the part is not in the inventory, which is a real answer. " +
			"Each row carries the parameters that matched, so there is no need to call get_part per candidate.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args specSearchArgs) (*mcp.CallToolResult, specSearchResult, error) {
		limit := args.Limit
		switch {
		case limit <= 0:
			limit = defaultSearchLimit
		case limit > 200:
			limit = 200
		}

		q := url.Values{}
		for key, v := range map[string]string{
			"value": args.Value, "package": args.Package,
			"parameter": args.Parameter, "search": args.Query, "category": args.Category,
		} {
			if s := strings.TrimSpace(v); s != "" {
				q.Set(key, s)
			}
		}
		q.Set("limit", strconv.Itoa(limit))

		matches, err := api.Get[[]apiSpecMatch](ctx, client, "/parts/search?"+q.Encode())
		if err != nil {
			return nil, specSearchResult{}, toolError(err)
		}

		out := make([]SpecMatch, 0, len(matches))
		for _, m := range matches {
			row := SpecMatch{PartSummary: toPartSummary(m.apiPart)}
			for _, p := range m.MatchedParameters {
				row.Matched = append(row.Matched, MatchedParameter{
					Name: p.TemplateName, Value: p.Value, Unit: deref(p.Units),
				})
			}
			if m.ReferenceOnly {
				row.ReferenceOnly = true
				row.Note = "recorded for reference; this part is not stocked"
			}
			out = append(out, row)
		}

		res := specSearchResult{Parts: out, Total: len(out)}
		if len(out) == 0 {
			res.Note = "Nothing in the inventory matches. That is an answer: the part is not held."
		}
		return nil, res, nil
	})
}

// ─── get_board ───────────────────────────────────────────────────────────────

type getBoardArgs struct {
	BoardID string `json:"board_id" jsonschema:"The board id (a UUID), as returned by get_project."`
}

// BOMLine is one line of a board's bill of materials.
type BOMLine struct {
	Refs         string `json:"refs"`
	Quantity     int    `json:"quantity"`
	Value        string `json:"value,omitempty"`
	Footprint    string `json:"footprint,omitempty"`
	MPN          string `json:"mpn,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	SupplierSKU  string `json:"supplier_sku,omitempty"`
	Description  string `json:"description,omitempty"`
	PartID       string `json:"part_id,omitempty"`
	PartName     string `json:"part_name,omitempty"`
	// Matched is false when no inventory part is linked to this line. Said
	// outright because an absent part_id is easy to skim past, and it is the
	// whole point of the line.
	Matched bool `json:"matched"`
}

type getBoardResult struct {
	Board    string    `json:"board"`
	BoardID  string    `json:"board_id"`
	Revision string    `json:"revision,omitempty"`
	Copies   int       `json:"copies,omitempty"`
	Lines    int       `json:"lines"`
	BOM      []BOMLine `json:"bom"`
	Note     string    `json:"note,omitempty"`
}

type apiBOMLine struct {
	Refs         string  `json:"refs"`
	Quantity     int     `json:"quantity"`
	Value        string  `json:"value"`
	Footprint    string  `json:"footprint"`
	MPN          string  `json:"mpn"`
	Manufacturer string  `json:"manufacturer"`
	SupplierSKU  string  `json:"supplier_sku"`
	Description  string  `json:"description"`
	PartID       *string `json:"part_id"`
	PartName     string  `json:"part_name"`
}

type apiBoardDetail struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Revision string       `json:"revision"`
	Copies   int          `json:"copies"`
	Lines    []apiBOMLine `json:"lines"`
}

// AddGetBoard wires the get_board tool.
func AddGetBoard(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_board",
		Description: "Get one board's bill of materials, line by line: the references (R7, C1), the value, the footprint, the manufacturer part number where the BOM carries one, and which inventory part each line is matched to. " +
			"Use this for any question about a specific component on a board, and read the mpn from the line rather than asking for it. " +
			"Use pick_list instead to ask whether the board can be built.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getBoardArgs) (*mcp.CallToolResult, getBoardResult, error) {
		id := strings.TrimSpace(args.BoardID)
		if id == "" {
			return nil, getBoardResult{}, fmt.Errorf("board_id is required")
		}
		b, err := api.Get[apiBoardDetail](ctx, client, "/boards/"+url.PathEscape(id))
		if err != nil {
			return nil, getBoardResult{}, toolError(err)
		}

		bom := make([]BOMLine, 0, len(b.Lines))
		unmatched := 0
		for _, l := range b.Lines {
			line := BOMLine{
				Refs: l.Refs, Quantity: l.Quantity, Value: l.Value,
				Footprint: l.Footprint, MPN: l.MPN, Manufacturer: l.Manufacturer,
				SupplierSKU: l.SupplierSKU, Description: truncateText(l.Description, 120),
				PartName: l.PartName,
			}
			if l.PartID != nil && *l.PartID != "" {
				line.PartID = *l.PartID
				line.Matched = true
			} else {
				unmatched++
			}
			bom = append(bom, line)
		}

		res := getBoardResult{
			Board: b.Name, BoardID: b.ID, Revision: b.Revision,
			Copies: b.Copies, Lines: len(bom), BOM: bom,
		}
		if unmatched > 0 {
			res.Note = fmt.Sprintf(
				"%d of %d lines are not linked to an inventory part. Those are unknowns, not parts that are held.",
				unmatched, len(bom))
		}
		return nil, res, nil
	})
}
