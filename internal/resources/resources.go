// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

// Package resources exposes the MCP resource surface of the FireBin MCP
// server: read-only views a client can pull on demand without spending a
// tool call.
//
// Resources pass the API's own JSON through unprojected, which is the one
// place this server does not trim. That is a deliberate split: the tools are
// what a model calls in a loop and so must stay context-cheap, while a
// resource is pulled deliberately by a client that wants the whole row. The
// tradeoff is that firebin://parts on a large inventory is a large document.
//
// v1 surface:
//
//	firebin://parts
//	firebin://part/{id}
//	firebin://locations
//	firebin://location/{id}
//	firebin://categories
//	firebin://stats
package resources

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/firelabsca/firebin-mcp/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterAll wires every resource this package owns onto the given MCP
// server. Mirrors tools.RegisterAll so cmd/mcp stays one line each.
func RegisterAll(srv *mcp.Server, client *api.Client) {
	addStatic(srv, client, "firebin://parts", "Parts", "All parts",
		"Every part in the inventory with its stock totals. Large on a big inventory; prefer the search_parts tool for anything targeted.",
		"/parts")
	addStatic(srv, client, "firebin://locations", "Locations", "All storage locations",
		"Every storage location (bin, drawer, shelf, cabinet), each with its parent and barcode.",
		"/locations")
	addStatic(srv, client, "firebin://categories", "Categories", "All part categories",
		"Every part category with the number of parts filed directly under it.",
		"/categories")
	addStatic(srv, client, "firebin://stats", "Stats", "Inventory summary",
		"Summary counts for the whole inventory: parts, variants, locations, low-stock parts, total units, and total value.",
		"/stats")

	addTemplated(srv, client, "firebin://part/{id}", "Part", "Part detail",
		"One part in full: parameters, manufacturer part numbers, supplier SKUs with pricing, and variants.",
		func(p map[string]string) string { return "/parts/" + url.PathEscape(p["id"]) })
	addTemplated(srv, client, "firebin://location/{id}", "Location", "Location contents",
		"The stock lots held in one storage location.",
		func(p map[string]string) string { return "/locations/" + url.PathEscape(p["id"]) + "/stock" })
}

// addStatic registers a fixed-URI resource backed by one API path.
func addStatic(srv *mcp.Server, client *api.Client, uri, name, title, description, path string) {
	srv.AddResource(&mcp.Resource{
		URI:         uri,
		Name:        name,
		Title:       title,
		Description: description,
		MIMEType:    "application/json",
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		raw, err := api.Get[json.RawMessage](ctx, client, path)
		if err != nil {
			return nil, apiError(uri, err)
		}
		return passthrough(uri, raw), nil
	})
}

// addTemplated registers a `{id}`-style resource template; pathFor turns the
// captured segments into the API path to fetch.
func addTemplated(srv *mcp.Server, client *api.Client, tmpl, name, title, description string, pathFor func(map[string]string) string) {
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: tmpl,
		Name:        name,
		Title:       title,
		Description: description,
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		params, ok := matchURI(req.Params.URI, tmpl)
		if !ok {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		raw, err := api.Get[json.RawMessage](ctx, client, pathFor(params))
		if err != nil {
			return nil, apiError(req.Params.URI, err)
		}
		return passthrough(req.Params.URI, raw), nil
	})
}

// matchURI parses a concrete URI against a template and returns the captured
// segments keyed by name. Template segments use `{name}` placeholders;
// literal segments must match exactly.
//
//	matchURI("firebin://part/abc", "firebin://part/{id}") → {"id": "abc"}, true
func matchURI(uri, template string) (map[string]string, bool) {
	uParts := strings.Split(uri, "/")
	tParts := strings.Split(template, "/")
	if len(uParts) != len(tParts) {
		return nil, false
	}
	out := map[string]string{}
	for i, t := range tParts {
		if strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}") {
			if uParts[i] == "" {
				return nil, false
			}
			out[t[1:len(t)-1]] = uParts[i]
			continue
		}
		if t != uParts[i] {
			return nil, false
		}
	}
	return out, true
}

func passthrough(uri string, raw json.RawMessage) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(raw),
		}},
	}
}

// apiError translates an API failure into the MCP error shape. A 404
// collapses to ResourceNotFoundError so the client treats it as a missing
// resource rather than a generic failure; everything else passes through.
func apiError(uri string, err error) error {
	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.Status == 404 {
		return mcp.ResourceNotFoundError(uri)
	}
	return err
}
