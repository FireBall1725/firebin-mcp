// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

// Package api is a minimal HTTP client for the FireBin REST API. Scoped to
// what the MCP tools call — deliberately hand-written rather than generated,
// because the committed OpenAPI spec annotates every request body as
// map[string]interface{} and so carries no usable body schemas.
//
// Unlike some sibling services, FireBin returns bare JSON: a successful call
// decodes straight into the target type, with no `data` envelope. Errors are
// uniform: {"error": "message", "code": "optional.dotted.id"}.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the thin FireBin HTTP client. Every outbound call carries the
// configured `fbin_pat_` bearer; non-2xx responses surface as *Error so the
// tool layer can map them onto MCP errors.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New builds a client against a base URL that already includes the /api/v1
// prefix (config.Load guarantees that).
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			// Enrichment calls fan out to Digi-Key and Nexar upstream, which
			// is the slowest thing this client waits on.
			Timeout: 45 * time.Second,
		},
	}
}

// Error is a non-2xx response from the API. Message and Code come from the
// API's own error envelope when it parses; Body keeps the raw payload for
// anything that doesn't (an ingress error page, say).
type Error struct {
	Status  int
	Message string
	Code    string
	Body    string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("firebin api %d: %s", e.Status, e.Message)
	}
	if e.Body != "" {
		return fmt.Sprintf("firebin api %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("firebin api %d", e.Status)
}

// statusIs reports whether err is an API error carrying the given status.
func statusIs(err error, status int) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == status
}

// NotFound reports whether err is an API 404. Callers use it to distinguish
// "no such part" from a transport failure.
func NotFound(err error) bool { return statusIs(err, http.StatusNotFound) }

// Forbidden reports whether err is an API 403 — either a viewer-role PAT
// hitting a write, or a non-admin PAT hitting an admin route.
func Forbidden(err error) bool { return statusIs(err, http.StatusForbidden) }

// Get decodes a GET response into T.
func Get[T any](ctx context.Context, c *Client, path string) (T, error) {
	var out T
	body, err := c.doRaw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("decoding %s: %w", path, err)
	}
	return out, nil
}

// Post sends a JSON body and decodes the response into Resp.
func Post[Req, Resp any](ctx context.Context, c *Client, path string, req Req) (Resp, error) {
	return send[Req, Resp](ctx, c, http.MethodPost, path, req)
}

// Patch mirrors Post for PATCH requests. Note that FireBin's PATCH bodies are
// full-object replacements validated with DisallowUnknownFields, so callers
// must send exactly the request struct — never an echoed-back model.
func Patch[Req, Resp any](ctx context.Context, c *Client, path string, req Req) (Resp, error) {
	return send[Req, Resp](ctx, c, http.MethodPatch, path, req)
}

func send[Req, Resp any](ctx context.Context, c *Client, method, path string, req Req) (Resp, error) {
	var out Resp
	buf, err := json.Marshal(req)
	if err != nil {
		return out, fmt.Errorf("encoding %s body: %w", path, err)
	}
	body, err := c.doRaw(ctx, method, path, buf)
	if err != nil {
		return out, err
	}
	// Some endpoints answer 200 with an empty body; leave the zero value.
	if len(bytes.TrimSpace(body)) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("decoding %s: %w", path, err)
	}
	return out, nil
}

// doRaw is the shared execute path, returning the raw response body.
func (c *Client) doRaw(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}

	apiErr := &Error{Status: resp.StatusCode, Body: string(raw)}
	var envelope struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(raw, &envelope) == nil {
		apiErr.Message, apiErr.Code = envelope.Error, envelope.Code
	}
	return nil, apiErr
}
