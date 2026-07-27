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

// apiScanResponse is what POST /scan returns. The parser handles three label
// shapes: an ECIA EIGP 114 Data Matrix off a distributor bag, an LCSC
// {key:value} QR, and a bare part number typed or scanned on its own.
type apiScanResponse struct {
	Parsed  *apiScanParsed `json:"parsed"`
	IsEIGP  bool           `json:"is_eigp"`
	Match   *apiScanMatch  `json:"match"`
	RawCode string         `json:"raw_code"`
}

// apiScanParsed mirrors eigp114.Parsed. The Fields map of every raw data
// identifier is deliberately left out of the projection below — it repeats
// what the named fields already carry.
type apiScanParsed struct {
	MPN             string `json:"mpn"`
	Quantity        int    `json:"quantity"`
	CustomerPart    string `json:"customer_part"`
	DistributorPart string `json:"distributor_part"`
	SalesOrder      string `json:"sales_order"`
	Invoice         string `json:"invoice"`
	PackingList     string `json:"packing_list"`
	CustomerPO      string `json:"customer_po"`
	DateCode        string `json:"date_code"`
	LotCode         string `json:"lot_code"`
	CountryOfOrigin string `json:"country_of_origin"`
	Distributor     string `json:"distributor"`
}

// ScannedLabel is the trimmed view handed to the model: the fields that
// matter when booking a delivery in.
type ScannedLabel struct {
	MPN             string `json:"mpn,omitempty"`
	Quantity        int    `json:"quantity,omitempty"`
	Distributor     string `json:"distributor,omitempty"`
	DistributorSKU  string `json:"distributor_sku,omitempty"`
	SalesOrder      string `json:"sales_order,omitempty"`
	CustomerPO      string `json:"customer_po,omitempty"`
	DateCode        string `json:"date_code,omitempty"`
	LotCode         string `json:"lot_code,omitempty"`
	CountryOfOrigin string `json:"country_of_origin,omitempty"`
}

func toScannedLabel(p *apiScanParsed) *ScannedLabel {
	if p == nil {
		return nil
	}
	return &ScannedLabel{
		MPN:             p.MPN,
		Quantity:        p.Quantity,
		Distributor:     p.Distributor,
		DistributorSKU:  firstNonEmptyLot(p.CustomerPart, p.DistributorPart),
		SalesOrder:      p.SalesOrder,
		CustomerPO:      p.CustomerPO,
		DateCode:        p.DateCode,
		LotCode:         p.LotCode,
		CountryOfOrigin: p.CountryOfOrigin,
	}
}

type apiScanMatch struct {
	PartID   string `json:"part_id"`
	PartName string `json:"part_name"`
}

type scanBarcodeArgs struct {
	Code string `json:"code" jsonschema:"The decoded barcode text, exactly as the scanner produced it. Accepts an ECIA EIGP 114 Data Matrix from a distributor bag, an LCSC QR code, a FireBin bin or lot barcode, or a bare manufacturer part number."`
}

type scanBarcodeResult struct {
	Kind     string        `json:"kind"`
	RawCode  string        `json:"raw_code"`
	Label    *ScannedLabel `json:"label,omitempty"`
	Part     *PartDetail   `json:"part,omitempty"`
	Location *Location     `json:"location,omitempty"`
	Stock    *StockLine    `json:"stock_lot,omitempty"`
	Note     string        `json:"note,omitempty"`
}

// AddScanBarcode wires the scan_barcode tool.
//
// FireBin has three separate scan endpoints because a scanned code can be
// three different things. This tool tries them in the order that costs least
// and is least ambiguous: a bin barcode and a lot barcode are exact lookups,
// and only a miss on both falls through to the distributor-label parser.
func AddScanBarcode(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "scan_barcode",
		Description: "Resolve a scanned barcode to whatever it identifies: a storage bin, a stock lot, or a distributor label. " +
			"For a distributor label (ECIA EIGP 114 Data Matrix or an LCSC QR) it parses out the manufacturer part number, quantity, order number, and date code, " +
			"and reports the matching inventory part if that MPN is already on file. Pass the decoded text exactly as scanned.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args scanBarcodeArgs) (*mcp.CallToolResult, scanBarcodeResult, error) {
		code := strings.TrimSpace(args.Code)
		if code == "" {
			return nil, scanBarcodeResult{}, fmt.Errorf("code is required")
		}

		// A bin barcode?
		if loc, err := api.Get[apiLocation](ctx, client, "/locations/scan?barcode="+url.QueryEscape(code)); err == nil {
			return nil, scanBarcodeResult{
				Kind:    "location",
				RawCode: code,
				Location: &Location{
					ID: loc.ID, Name: loc.Name, Path: loc.Name,
					ParentID: deref(loc.ParentID), Barcode: deref(loc.Barcode),
					Description: deref(loc.Description),
				},
				Note: "This is a storage bin. Use location_contents with this location id to see what is in it.",
			}, nil
		} else if !api.NotFound(err) {
			return nil, scanBarcodeResult{}, toolError(err)
		}

		// A stock lot barcode (a cut mini spool, say)?
		if item, err := api.Get[apiStockItem](ctx, client, "/stock/scan?barcode="+url.QueryEscape(code)); err == nil {
			line := toStockLine(item)
			res := scanBarcodeResult{
				Kind:    "stock_lot",
				RawCode: code,
				Stock:   &line,
				Note:    fmt.Sprintf("This is a stock lot of %q.", item.PartName),
			}
			if part, err := api.Get[apiPart](ctx, client, "/parts/"+url.PathEscape(item.PartID)); err == nil {
				d := toPartDetail(part, nil)
				res.Part = &d
			}
			return nil, res, nil
		} else if !api.NotFound(err) {
			return nil, scanBarcodeResult{}, toolError(err)
		}

		// Otherwise it is a distributor label or a bare part number.
		resp, err := api.Post[map[string]string, apiScanResponse](ctx, client, "/scan", map[string]string{"code": code})
		if err != nil {
			return nil, scanBarcodeResult{}, toolError(err)
		}

		res := scanBarcodeResult{Kind: "distributor_label", RawCode: resp.RawCode, Label: toScannedLabel(resp.Parsed)}
		if !resp.IsEIGP {
			res.Kind = "part_number"
		}
		if res.RawCode == "" {
			res.RawCode = code
		}

		if resp.Match != nil && resp.Match.PartID != "" {
			part, err := api.Get[apiPart](ctx, client, "/parts/"+url.PathEscape(resp.Match.PartID))
			if err == nil {
				stock, _ := api.Get[[]apiStockItem](ctx, client, "/parts/"+url.PathEscape(resp.Match.PartID)+"/stock")
				d := toPartDetail(part, stock)
				res.Part = &d
			}
			res.Note = "This part is already in the inventory. Use adjust_stock with kind 'add' to book in the scanned quantity."
			return nil, res, nil
		}

		res.Note = "No inventory part matches this code yet. Use add_part_by_mpn with the parsed manufacturer part number to create one, or lookup_mpn to see the distributor data first."
		return nil, res, nil
	})
}
