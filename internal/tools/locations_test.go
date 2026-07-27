// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import "testing"

func TestBuildPaths(t *testing.T) {
	locs := []apiLocation{
		{ID: "shop", Name: "Shop"},
		{ID: "cab", Name: "Cabinet A", ParentID: ptr("shop")},
		{ID: "drawer", Name: "Drawer 3", ParentID: ptr("cab")},
		{ID: "orphan", Name: "Loose Bin", ParentID: ptr("does-not-exist")},
	}

	paths := buildPaths(locs)

	want := map[string]string{
		"shop":   "Shop",
		"cab":    "Shop / Cabinet A",
		"drawer": "Shop / Cabinet A / Drawer 3",
		// A dangling parent falls back to the bare name rather than
		// dropping the location.
		"orphan": "Loose Bin",
	}
	for id, w := range want {
		if paths[id] != w {
			t.Errorf("path[%s] = %q, want %q", id, paths[id], w)
		}
	}
}

// A parent cycle should never come out of the API, but it must not hang the
// server if it ever does.
func TestBuildPathsSurvivesCycle(t *testing.T) {
	locs := []apiLocation{
		{ID: "a", Name: "A", ParentID: ptr("b")},
		{ID: "b", Name: "B", ParentID: ptr("a")},
	}

	done := make(chan map[string]string, 1)
	go func() { done <- buildPaths(locs) }()

	paths := <-done
	if paths["a"] == "" || paths["b"] == "" {
		t.Errorf("cycle produced empty paths: %+v", paths)
	}
}

func TestBuildCategoryPaths(t *testing.T) {
	cats := []apiCategory{
		{ID: "passive", Name: "Passives"},
		{ID: "res", Name: "Resistors", ParentID: ptr("passive")},
	}
	paths := buildCategoryPaths(cats)
	if paths["res"] != "Passives / Resistors" {
		t.Errorf("path[res] = %q, want %q", paths["res"], "Passives / Resistors")
	}
}
