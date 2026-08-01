// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import (
	"strings"
)

// ─── Raw wire shapes ─────────────────────────────────────────────────────────
//
// These mirror the JSON the FireBin API actually returns (internal/models in
// firebin-api). They are deliberately kept separate from the structs handed
// back to the model: a Part carries variants, parameters, manufacturer parts
// and alternatives, and there is no pagination on any list endpoint, so
// returning raw rows would flood the context window.

type apiPart struct {
	ID          string  `json:"id"`
	CategoryID  *string `json:"category_id"`
	VariantOf   *string `json:"variant_of"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IPN         *string `json:"ipn"`
	Package     *string `json:"package"`
	Keywords    *string `json:"keywords"`
	Barcode     *string `json:"barcode"`
	ImagePath   *string `json:"image_path"`
	IsTemplate  bool    `json:"is_template"`
	IsAssembly  bool    `json:"is_assembly"`
	// ReferenceOnly marks a part recorded but not owned: researched, or worth
	// remembering, but never stocked. Without it such a part reads as "in
	// stock: 0", which is sold out rather than never held.
	ReferenceOnly       bool             `json:"reference_only"`
	MinimumStock        float64          `json:"minimum_stock"`
	DefaultLocationID   *string          `json:"default_location_id"`
	TotalStock          float64          `json:"total_stock"`
	VariantCount        int              `json:"variant_count"`
	PrimaryMPN          *string          `json:"primary_mpn"`
	PrimaryManufacturer *string          `json:"primary_manufacturer"`
	PrimaryLocation     *string          `json:"primary_location"`
	PrimaryLocationID   *string          `json:"primary_location_id"`
	Parameters          []apiPartParam   `json:"parameters"`
	ManufacturerParts   []apiManufPart   `json:"manufacturer_parts"`
	Alternatives        []apiAlternative `json:"alternatives"`
	Variants            []apiPart        `json:"variants"`
	CategoryName        *string          `json:"category_name"`
}

type apiPartParam struct {
	TemplateName string  `json:"template_name"`
	Units        *string `json:"units"`
	Value        string  `json:"value"`
}

type apiManufPart struct {
	ID               string            `json:"id"`
	ManufacturerName *string           `json:"manufacturer_name"`
	MPN              string            `json:"mpn"`
	Description      *string           `json:"description"`
	DatasheetURL     *string           `json:"datasheet_url"`
	SupplierParts    []apiSupplierPart `json:"supplier_parts"`
}

type apiSupplierPart struct {
	ID           string          `json:"id"`
	SupplierName string          `json:"supplier_name"`
	SKU          string          `json:"sku"`
	Packaging    *string         `json:"packaging"`
	MOQ          *float64        `json:"moq"`
	URL          *string         `json:"url"`
	Pricing      []apiPriceBreak `json:"pricing"`
}

type apiPriceBreak struct {
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
}

type apiAlternative struct {
	MPN          string `json:"mpn"`
	Manufacturer string `json:"manufacturer"`
	Description  string `json:"description"`
}

type apiLocation struct {
	ID          string  `json:"id"`
	ParentID    *string `json:"parent_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Barcode     *string `json:"barcode"`
}

type apiCategory struct {
	ID          string  `json:"id"`
	ParentID    *string `json:"parent_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	PartCount   int     `json:"part_count"`
}

type apiStockItem struct {
	ID            string   `json:"id"`
	PartID        string   `json:"part_id"`
	PartName      string   `json:"part_name"`
	LocationID    *string  `json:"location_id"`
	LocationName  *string  `json:"location_name"`
	Quantity      float64  `json:"quantity"`
	Status        string   `json:"status"`
	Batch         *string  `json:"batch"`
	Serial        *string  `json:"serial"`
	Note          *string  `json:"note"`
	Barcode       *string  `json:"barcode"`
	Name          *string  `json:"name"`
	PurchasePrice *float64 `json:"purchase_price"`
}

type apiStockTxn struct {
	ID                string  `json:"id"`
	StockItemID       string  `json:"stock_item_id"`
	PartID            *string `json:"part_id"`
	PartName          *string `json:"part_name"`
	Kind              string  `json:"kind"`
	Delta             float64 `json:"delta"`
	ResultingQuantity float64 `json:"resulting_quantity"`
	FromLocationName  *string `json:"from_location_name"`
	ToLocationName    *string `json:"to_location_name"`
	Note              *string `json:"note"`
	CreatedAt         string  `json:"created_at"`
}

type apiStats struct {
	PartsCount     int     `json:"parts_count"`
	VariantsCount  int     `json:"variants_count"`
	LocationsCount int     `json:"locations_count"`
	LowStockCount  int     `json:"low_stock_count"`
	TotalUnits     float64 `json:"total_units"`
	InventoryValue float64 `json:"inventory_value"`
}

// ─── LLM-facing projections ──────────────────────────────────────────────────

// PartSummary is the compact row returned by search_parts and low_stock.
// Enough to answer "do I have one, how many, and where" without a follow-up
// call; get_part fetches the rest.
type PartSummary struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	IPN          string  `json:"ipn,omitempty"`
	MPN          string  `json:"mpn,omitempty"`
	Manufacturer string  `json:"manufacturer,omitempty"`
	Package      string  `json:"package,omitempty"`
	Description  string  `json:"description,omitempty"`
	TotalStock   float64 `json:"total_stock"`
	MinimumStock float64 `json:"minimum_stock,omitempty"`
	Location     string  `json:"location,omitempty"`
	VariantCount int     `json:"variant_count,omitempty"`
}

func toPartSummary(p apiPart) PartSummary {
	return PartSummary{
		ID:           p.ID,
		Name:         p.Name,
		IPN:          deref(p.IPN),
		MPN:          deref(p.PrimaryMPN),
		Manufacturer: deref(p.PrimaryManufacturer),
		Package:      deref(p.Package),
		Description:  truncateText(deref(p.Description), 160),
		TotalStock:   p.TotalStock,
		MinimumStock: p.MinimumStock,
		Location:     deref(p.PrimaryLocation),
		VariantCount: p.VariantCount,
	}
}

func toPartSummaries(in []apiPart) []PartSummary {
	out := make([]PartSummary, 0, len(in))
	for _, p := range in {
		out = append(out, toPartSummary(p))
	}
	return out
}

// Parameter is one spec row ("Capacitance: 100 nF").
type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Units string `json:"units,omitempty"`
}

// SupplierOffer is a distributor SKU with its price breaks.
//
// The breaks are structured rather than flattened into "1: 0.42, 10: 0.31".
// A caller comparing suppliers has to do arithmetic on these, and it has to
// know the currency of each: Digi-Key and Mouser honour the instance currency
// setting but Nexar stores whatever each seller reported, so two rows on the
// same part can be in different currencies and comparing them as bare numbers
// compares different things. The flattened string was cheaper in tokens and
// invited exactly that mistake.
type SupplierOffer struct {
	Supplier    string       `json:"supplier"`
	SKU         string       `json:"sku"`
	MOQ         float64      `json:"moq,omitempty"`
	Packaging   string       `json:"packaging,omitempty"`
	PriceBreaks []PriceBreak `json:"price_breaks,omitempty"`
	URL         string       `json:"url,omitempty"`
}

// PriceBreak is one quantity tier, with the currency it is quoted in.
type PriceBreak struct {
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency,omitempty"`
}

// ManufacturerPart is one MPN on a part, with its distributor offers.
type ManufacturerPart struct {
	ID           string          `json:"id"`
	Manufacturer string          `json:"manufacturer,omitempty"`
	MPN          string          `json:"mpn"`
	Datasheet    string          `json:"datasheet,omitempty"`
	Suppliers    []SupplierOffer `json:"suppliers,omitempty"`
}

// StockLine is one lot of a part in one bin.
type StockLine struct {
	StockItemID string  `json:"stock_item_id"`
	Location    string  `json:"location,omitempty"`
	LocationID  string  `json:"location_id,omitempty"`
	Quantity    float64 `json:"quantity"`
	LotName     string  `json:"lot_name,omitempty"`
	LotBarcode  string  `json:"lot_barcode,omitempty"`
	Status      string  `json:"status,omitempty"`
	Note        string  `json:"note,omitempty"`
}

func toStockLine(s apiStockItem) StockLine {
	return StockLine{
		StockItemID: s.ID,
		Location:    deref(s.LocationName),
		LocationID:  deref(s.LocationID),
		Quantity:    s.Quantity,
		LotName:     deref(s.Name),
		LotBarcode:  deref(s.Barcode),
		Status:      s.Status,
		Note:        deref(s.Note),
	}
}

// PartDetail is the full shape from get_part: specs, MPNs with distributor
// pricing, and the per-bin stock breakdown, so the model can answer follow-up
// questions without another round trip.
type PartDetail struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	IPN          string             `json:"ipn,omitempty"`
	Package      string             `json:"package,omitempty"`
	Keywords     string             `json:"keywords,omitempty"`
	Category     string             `json:"category,omitempty"`
	CategoryID   string             `json:"category_id,omitempty"`
	Barcode      string             `json:"barcode,omitempty"`
	IsAssembly   bool               `json:"is_assembly,omitempty"`
	IsTemplate   bool               `json:"is_template,omitempty"`
	TotalStock   float64            `json:"total_stock"`
	MinimumStock float64            `json:"minimum_stock,omitempty"`
	BelowMinimum bool               `json:"below_minimum,omitempty"`
	Parameters   []Parameter        `json:"parameters,omitempty"`
	Manufacturer []ManufacturerPart `json:"manufacturer_parts,omitempty"`
	Stock        []StockLine        `json:"stock,omitempty"`
	Variants     []PartSummary      `json:"variants,omitempty"`
	Alternatives []apiAlternative   `json:"alternatives,omitempty"`
}

func toPartDetail(p apiPart, stock []apiStockItem) PartDetail {
	d := PartDetail{
		ID:           p.ID,
		Name:         p.Name,
		Description:  deref(p.Description),
		IPN:          deref(p.IPN),
		Package:      deref(p.Package),
		Keywords:     deref(p.Keywords),
		Category:     deref(p.CategoryName),
		CategoryID:   deref(p.CategoryID),
		Barcode:      deref(p.Barcode),
		IsAssembly:   p.IsAssembly,
		IsTemplate:   p.IsTemplate,
		TotalStock:   p.TotalStock,
		MinimumStock: p.MinimumStock,
		BelowMinimum: p.MinimumStock > 0 && p.TotalStock < p.MinimumStock,
		Alternatives: p.Alternatives,
	}
	for _, param := range p.Parameters {
		d.Parameters = append(d.Parameters, Parameter{
			Name:  param.TemplateName,
			Value: param.Value,
			Units: deref(param.Units),
		})
	}
	for _, mp := range p.ManufacturerParts {
		m := ManufacturerPart{
			ID:           mp.ID,
			Manufacturer: deref(mp.ManufacturerName),
			MPN:          mp.MPN,
			Datasheet:    deref(mp.DatasheetURL),
		}
		for _, sp := range mp.SupplierParts {
			m.Suppliers = append(m.Suppliers, SupplierOffer{
				Supplier:    sp.SupplierName,
				SKU:         sp.SKU,
				MOQ:         derefFloat(sp.MOQ),
				PriceBreaks: toPriceBreaks(sp.Pricing),
				URL:         deref(sp.URL),
			})
		}
		d.Manufacturer = append(d.Manufacturer, m)
	}
	for _, s := range stock {
		d.Stock = append(d.Stock, toStockLine(s))
	}
	for _, v := range p.Variants {
		d.Variants = append(d.Variants, toPartSummary(v))
	}
	return d
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// toPriceBreaks keeps each tier as a number with its own currency.
func toPriceBreaks(breaks []apiPriceBreak) []PriceBreak {
	if len(breaks) == 0 {
		return nil
	}
	out := make([]PriceBreak, 0, len(breaks))
	for _, b := range breaks {
		// Field-identical to apiPriceBreak; the conversion breaks at compile
		// time if the API's shape ever drifts from ours.
		out = append(out, PriceBreak(b))
	}
	return out
}

// truncateText caps a free-text field so a long provider description can't
// dominate a list of 25 results.
func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "…"
}

// capList trims a slice to limit and reports the original length, so every
// list tool can say plainly that it truncated. The API paginates nothing, so
// this is the only thing standing between a large inventory and the context
// window.
func capList[T any](in []T, limit int) (out []T, total int, truncated bool) {
	total = len(in)
	if limit > 0 && total > limit {
		return in[:limit], total, true
	}
	return in, total, false
}
