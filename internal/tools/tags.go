// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/firelabsca/firebin-mcp/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ─── list_tags ───────────────────────────────────────────────────────────────

type listTagsResult struct {
	Tags  []TagSummary `json:"tags"`
	Total int          `json:"total"`
}

// TagSummary is the compact row returned by list_tags.
type TagSummary struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	PartCount int    `json:"part_count"`
}

// AddListTags wires the list_tags tool.
func AddListTags(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_tags",
		Description: "List every tag in use, with how many parts carry each. " +
			"Tags are the informal names a part answers to besides its own: a JST SH 1.0 mm header tagged 'Qwiic' and 'STEMMA QT' is findable by either word even though neither appears in its part number. " +
			"Call this before tag_part so you reuse a tag that already exists instead of coining a second spelling of it; the server folds spellings together, so 'STEMMA QT' and 'stemma-qt' are one tag either way.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listTagsResult, error) {
		tags, err := api.Get[[]apiTag](ctx, client, "/tags")
		if err != nil {
			return nil, listTagsResult{}, toolError(err)
		}
		out := make([]TagSummary, 0, len(tags))
		for _, t := range tags {
			out = append(out, TagSummary{Name: t.Name, Slug: t.Slug, PartCount: t.PartCount})
		}
		return nil, listTagsResult{Tags: out, Total: len(out)}, nil
	})
}

// ─── tag_part ────────────────────────────────────────────────────────────────

type tagPartArgs struct {
	ID     string   `json:"id" jsonschema:"The part id (a UUID), as returned by search_parts or scan_barcode."`
	Add    []string `json:"add,omitempty" jsonschema:"Tag names to put on the part. A name that does not exist yet is created. Spellings fold together, so adding 'qwiic' to a part when the tag 'Qwiic' exists reuses it rather than making a second one."`
	Remove []string `json:"remove,omitempty" jsonschema:"Tag names to take off this part. The tag itself stays in the vocabulary for other parts. Removing a tag the part does not carry is not an error."`
}

type tagPartResult struct {
	PartID string   `json:"part_id"`
	Tags   []string `json:"tags"`
}

// AddTagPart wires the tag_part tool.
//
// Add and remove rather than a full replacement, deliberately. A caller working
// from an instruction like "call this one qwiic" does not know the rest of the
// set, and a replace from such a caller silently drops every tag it had not been
// told about. The API has a replace endpoint; this tool does not expose it.
func AddTagPart(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "tag_part",
		Description: "Add or remove tags on a part. Tags are the informal names a part answers to besides its own, e.g. 'Qwiic' or 'STEMMA QT' on a JST SH connector, and searching for one finds the part. " +
			"A tag never replaces the part's name, IPN or manufacturer part number; use update_part for those. " +
			"Adding a tag that does not exist creates it, so call list_tags first to reuse the vocabulary already in use. " +
			"Tags not named in add or remove are left alone.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tagPartArgs) (*mcp.CallToolResult, tagPartResult, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return nil, tagPartResult{}, fmt.Errorf("id is required")
		}
		if len(args.Add) == 0 && len(args.Remove) == 0 {
			return nil, tagPartResult{}, fmt.Errorf("pass add, remove, or both")
		}

		// The API replaces the whole set, so build the new set from the current
		// one. Reading first is what makes add and remove additive.
		current, err := api.Get[apiPart](ctx, client, "/parts/"+url.PathEscape(id))
		if err != nil {
			return nil, tagPartResult{}, toolError(err)
		}

		drop := map[string]bool{}
		for _, r := range args.Remove {
			drop[tagFold(r)] = true
		}
		next := []string{}
		have := map[string]bool{}
		for _, t := range current.Tags {
			if drop[tagFold(t.Name)] {
				continue
			}
			next = append(next, t.Name)
			have[tagFold(t.Name)] = true
		}
		for _, a := range args.Add {
			f := tagFold(a)
			if f == "" || have[f] || drop[f] {
				continue
			}
			have[f] = true
			next = append(next, strings.TrimSpace(a))
		}

		updated, err := api.Put[setTagsRequest, []apiTag](ctx, client,
			"/parts/"+url.PathEscape(id)+"/tags", setTagsRequest{Tags: next})
		if err != nil {
			return nil, tagPartResult{}, toolError(err)
		}
		return nil, tagPartResult{PartID: id, Tags: tagNames(updated)}, nil
	})
}

type setTagsRequest struct {
	Tags []string `json:"tags"`
}

// tagFold mirrors TagSlug in the API: lowercased, letters and digits only. Used
// here only to decide whether two names the caller passed are the same tag, so
// "add Qwiic, remove qwiic" does not end up doing both.
func tagFold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
