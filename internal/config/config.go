// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

// Package config resolves the MCP server's runtime configuration from env
// vars and a small on-disk state file. Keeps the three-layer inbound-token
// resolution ("env → persisted file → first-run generate") in one place so
// cmd/mcp and any future integration tests share the same logic.
package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

// MCPTokenPrefix identifies inbound MCP tokens this server mints for its
// clients. Distinct from the FireBin API's own `fbin_pat_` personal access
// tokens so a leaked credential is greppable and unambiguous about which
// side of the connection it belongs to.
const MCPTokenPrefix = "fbin_mcp_"

const mcpTokenRandomLen = 43

// Config is the fully-resolved runtime shape, ready to hand to the server
// wiring.
type Config struct {
	// APIURL is the base URL of the FireBin API, including the version
	// prefix — e.g. "http://firebin-api:8080/api/v1". No trailing slash.
	APIURL string

	// AccessToken is the `fbin_pat_*` credential used on outbound API calls.
	// Minted in the FireBin web UI under Settings, API tokens. It inherits
	// the role of the user that minted it, so mint it from a dedicated
	// non-admin account.
	AccessToken string

	// Listen is the address the MCP server binds to (e.g. ":8090").
	Listen string

	// MCPToken is the bearer credential incoming MCP clients must present.
	// Resolved from FIREBIN_MCP_TOKEN env → <data dir>/mcp-token →
	// first-run generation (in which case FirstRun is true and the caller is
	// expected to print the reveal banner).
	MCPToken string

	// TokenFilePath is where a generated token is stored. Callers put it in
	// the banner so users know where to look if they lose the token.
	TokenFilePath string

	// FirstRun indicates the MCP token was freshly generated this boot and
	// the user needs to see it at least once.
	FirstRun bool
}

// apiVersionPath is appended when the operator supplies a bare origin. The
// FireBin API serves everything except /health, /api/docs and
// /api/openapi.json under this prefix.
const apiVersionPath = "/api/v1"

// Load resolves config from the environment.
func Load() (*Config, error) {
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("FIREBIN_API_URL")), "/")
	if raw == "" {
		return nil, errors.New("FIREBIN_API_URL is required (e.g. http://firebin-api:8080)")
	}
	// Accept either an origin or a full base path. Appending the version
	// prefix ourselves means a deployer can set the plain service URL and
	// still get working calls, and pasting the full base path also works.
	apiURL := raw
	if !strings.HasSuffix(apiURL, apiVersionPath) {
		apiURL += apiVersionPath
	}

	pat := strings.TrimSpace(os.Getenv("FIREBIN_ACCESS_TOKEN"))
	if pat == "" {
		return nil, errors.New("FIREBIN_ACCESS_TOKEN is required (mint one in the FireBin web UI under Settings, API tokens)")
	}

	listen := strings.TrimSpace(os.Getenv("FIREBIN_MCP_LISTEN"))
	if listen == "" {
		listen = ":8090"
	}

	dataDir := strings.TrimSpace(os.Getenv("FIREBIN_MCP_DATA_DIR"))
	if dataDir == "" {
		dataDir = "/data"
	}
	tokenFile := filepath.Join(dataDir, "mcp-token")

	mcpTok, firstRun, err := resolveMCPToken(tokenFile)
	if err != nil {
		return nil, err
	}

	return &Config{
		APIURL:        apiURL,
		AccessToken:   pat,
		Listen:        listen,
		MCPToken:      mcpTok,
		TokenFilePath: tokenFile,
		FirstRun:      firstRun,
	}, nil
}

// resolveMCPToken implements the three-tier fallback. Returns (token,
// firstRun, err); firstRun is true only when we generated and persisted a
// fresh token on this boot.
func resolveMCPToken(tokenFile string) (string, bool, error) {
	// 1. Env var overrides everything. Declarative setups (k8s SealedSecret,
	//    compose with a secret) land here and never touch the disk.
	if env := strings.TrimSpace(os.Getenv("FIREBIN_MCP_TOKEN")); env != "" {
		return env, false, nil
	}

	// 2. Persisted file from a previous boot.
	if raw, err := os.ReadFile(tokenFile); err == nil {
		if v := strings.TrimSpace(string(raw)); v != "" {
			return v, false, nil
		}
	} else if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("reading token file %q: %w", tokenFile, err)
	}

	// 3. First run — generate, persist, and flag for the banner.
	body, err := randomBase62(mcpTokenRandomLen)
	if err != nil {
		return "", false, fmt.Errorf("generating MCP token: %w", err)
	}
	token := MCPTokenPrefix + body

	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		return "", false, fmt.Errorf("creating token dir: %w", err)
	}
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		return "", false, fmt.Errorf("writing token file %q: %w", tokenFile, err)
	}
	return token, true, nil
}

// randomBase62 returns a cryptographically-random string of length n using
// the base62 alphabet.
func randomBase62(n int) (string, error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	max := big.NewInt(int64(len(alphabet)))
	buf := make([]byte, n)
	for i := range n {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = alphabet[idx.Int64()]
	}
	return string(buf), nil
}
