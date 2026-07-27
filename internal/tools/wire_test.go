// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import "testing"

func ptr[T any](v T) *T { return &v }

func TestCapList(t *testing.T) {
	in := []int{1, 2, 3, 4, 5}

	got, total, truncated := capList(in, 3)
	if len(got) != 3 || total != 5 || !truncated {
		t.Errorf("capList(5 items, 3) = %v, %d, %v; want 3 items, 5, true", got, total, truncated)
	}

	got, total, truncated = capList(in, 10)
	if len(got) != 5 || total != 5 || truncated {
		t.Errorf("capList(5 items, 10) = %v, %d, %v; want 5 items, 5, false", got, total, truncated)
	}

	// A zero limit means "no cap" — callers substitute their own default
	// before calling, so this must not silently return nothing.
	got, total, truncated = capList(in, 0)
	if len(got) != 5 || total != 5 || truncated {
		t.Errorf("capList(5 items, 0) = %v, %d, %v; want 5 items, 5, false", got, total, truncated)
	}
}

func TestFormatPriceBreaks(t *testing.T) {
	cases := []struct {
		name string
		in   []apiPriceBreak
		want string
	}{
		{"empty", nil, ""},
		{
			"integer quantities keep no decimal tail",
			[]apiPriceBreak{{Quantity: 1, Price: 0.42, Currency: "CAD"}, {Quantity: 100, Price: 0.31, Currency: "CAD"}},
			"1: 0.4200 CAD, 100: 0.3100 CAD",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatPriceBreaks(c.in); got != c.want {
				t.Errorf("formatPriceBreaks() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	if got := truncateText("short", 20); got != "short" {
		t.Errorf("under the cap should pass through, got %q", got)
	}
	got := truncateText("0123456789abcdef", 10)
	if got != "0123456789…" {
		t.Errorf("truncateText() = %q, want %q", got, "0123456789…")
	}
}

// A part is only below minimum when a minimum was actually set. A part with
// minimum_stock 0 and no stock must not be reported as short, or every
// unconfigured part in the inventory looks like a reorder.
func TestToPartDetailBelowMinimum(t *testing.T) {
	cases := []struct {
		name    string
		minimum float64
		total   float64
		want    bool
	}{
		{"no minimum configured, no stock", 0, 0, false},
		{"below a configured minimum", 10, 3, true},
		{"exactly at the minimum is not below it", 10, 10, false},
		{"above the minimum", 10, 25, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := toPartDetail(apiPart{MinimumStock: c.minimum, TotalStock: c.total}, nil)
			if d.BelowMinimum != c.want {
				t.Errorf("BelowMinimum = %v, want %v", d.BelowMinimum, c.want)
			}
		})
	}
}

// fromPart must reproduce the API's partRequest field set exactly. PATCH is a
// full-object replacement decoded with DisallowUnknownFields, so a dropped
// field silently wipes data and a stray field is a 400.
func TestFromPartPreservesEverything(t *testing.T) {
	src := apiPart{
		ID:                "ignored",
		CategoryID:        ptr("cat-1"),
		Name:              "22 Ω Resistor",
		Description:       ptr("thick film"),
		IPN:               ptr("R-0001"),
		Package:           ptr("0603"),
		Keywords:          ptr("resistor smd"),
		Barcode:           ptr("BC-1"),
		ImagePath:         ptr("/img/r.svg"),
		IsTemplate:        true,
		IsAssembly:        false,
		MinimumStock:      50,
		DefaultLocationID: ptr("loc-1"),
		Parameters: []apiPartParam{
			{TemplateName: "Resistance", Value: "22", Units: ptr("Ω")},
		},
		// Fields that exist on the model but must never reach the request.
		TotalStock:   1234,
		VariantCount: 7,
	}

	req := fromPart(src)

	if req.Name != src.Name || deref(req.IPN) != "R-0001" || deref(req.Package) != "0603" {
		t.Errorf("scalar fields not carried across: %+v", req)
	}
	if req.MinimumStock != 50 || !req.IsTemplate || deref(req.DefaultLocationID) != "loc-1" {
		t.Errorf("flags and thresholds not carried across: %+v", req)
	}
	if len(req.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(req.Parameters))
	}
	// The model calls it template_name; the request calls it name.
	if req.Parameters[0].Name != "Resistance" || req.Parameters[0].Value != "22" || deref(req.Parameters[0].Units) != "Ω" {
		t.Errorf("parameter not mapped correctly: %+v", req.Parameters[0])
	}
}

// A part with no parameters must still send an empty array, not null: the
// repository replaces the parameter set wholesale from this field.
func TestFromPartEmptyParametersIsNotNil(t *testing.T) {
	req := fromPart(apiPart{Name: "x"})
	if req.Parameters == nil {
		t.Error("Parameters must marshal as [], not null")
	}
}
