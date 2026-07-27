// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/firelabsca/firebin-mcp/internal/api"
)

// toolError turns an API failure into something the model can act on rather
// than a bare status code. The API already writes a human-readable message in
// every error envelope, so the job here is to add the context the model needs
// to decide what to do next: retry with a different id, stop asking for
// writes, or tell the user their token is wrong.
func toolError(err error) error {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	detail := reason(apiErr)
	switch apiErr.Status {
	case 401:
		return fmt.Errorf("the FireBin API rejected this server's access token (%s). The FIREBIN_ACCESS_TOKEN personal access token is missing, expired, revoked, or belongs to a disabled account", detail)
	case 403:
		return fmt.Errorf("the FireBin API refused this operation: %s. This server's account does not have permission to make this change; read operations will still work", detail)
	case 404:
		return fmt.Errorf("not found in FireBin: %s", detail)
	case 502, 503, 504:
		return fmt.Errorf("FireBin could not reach an upstream service: %s. This usually means an enrichment provider (Digi-Key or Nexar) is unconfigured or down", detail)
	default:
		return apiErr
	}
}

// reason produces a non-empty explanation even when the API's JSON envelope
// never arrived. That happens when something between here and the API answers
// first — a reverse proxy or ingress with its own error pages will replace a
// JSON 404 body with an HTML one — so the status is all we can trust.
func reason(apiErr *api.Error) string {
	if apiErr.Message != "" {
		return apiErr.Message
	}
	if body := strings.TrimSpace(apiErr.Body); body != "" && !strings.Contains(body, "<") {
		return truncate(body, 200)
	}
	return fmt.Sprintf("the API returned HTTP %d with no error detail", apiErr.Status)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
