// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/firelabsca/firebin-mcp/internal/api"
)

func TestToolErrorUsesTheAPIMessage(t *testing.T) {
	err := toolError(&api.Error{Status: 403, Message: "read-only account"})
	if !strings.Contains(err.Error(), "read-only account") {
		t.Errorf("expected the API's own message to survive, got %q", err)
	}
}

// An ingress or reverse proxy can answer before the API does and replace the
// JSON envelope with an HTML page. The tool error must still say something
// useful rather than trailing off after a colon.
func TestToolErrorWithNoEnvelope(t *testing.T) {
	cases := []struct {
		name string
		in   *api.Error
	}{
		{"empty body", &api.Error{Status: 404}},
		{"html body", &api.Error{Status: 404, Body: "<html>404 Not Found</html>"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := toolError(c.in).Error()
			if strings.HasSuffix(strings.TrimSpace(got), ":") {
				t.Errorf("error trails off with no detail: %q", got)
			}
			if !strings.Contains(got, "404") {
				t.Errorf("expected the status to appear when there is no message, got %q", got)
			}
		})
	}
}

// A plain-text body is worth surfacing; an HTML one is noise.
func TestToolErrorKeepsPlainTextBody(t *testing.T) {
	got := toolError(&api.Error{Status: 404, Body: "no location with that barcode"}).Error()
	if !strings.Contains(got, "no location with that barcode") {
		t.Errorf("plain-text body should be surfaced, got %q", got)
	}
}

// Anything that is not an API error passes through untouched — a DNS failure
// or a timeout must not be dressed up as a permissions problem.
func TestToolErrorPassesThroughNonAPIErrors(t *testing.T) {
	orig := errors.New("dial tcp: connection refused")
	if got := toolError(orig); got != orig {
		t.Errorf("expected the original error, got %v", got)
	}
}

// A status with no special handling keeps the *api.Error so callers can still
// inspect it.
func TestToolErrorPreservesUnhandledStatuses(t *testing.T) {
	in := &api.Error{Status: 400, Message: "quantity must be greater than zero"}
	var out *api.Error
	if !errors.As(toolError(in), &out) || out.Status != 400 {
		t.Errorf("expected the *api.Error to survive for a 400, got %v", toolError(in))
	}
}
