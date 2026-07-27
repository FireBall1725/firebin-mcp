// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

// Command mcp is the FireBin MCP server entrypoint. It presents a Model
// Context Protocol surface over streamable HTTP, translating each tool call
// into an authenticated request against the FireBin REST API. It holds no
// database credentials and has no special backend access; it is just another
// API client, and inherits the API's own authorization and validation.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/firelabsca/firebin-mcp/internal/api"
	"github.com/firelabsca/firebin-mcp/internal/config"
	"github.com/firelabsca/firebin-mcp/internal/resources"
	"github.com/firelabsca/firebin-mcp/internal/tools"
	"github.com/firelabsca/firebin-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("startup config invalid", "error", err)
		os.Exit(1)
	}

	logger.Info("firebin-mcp", "version", version.BuildVersion, "listen", cfg.Listen, "upstream", cfg.APIURL)
	if cfg.FirstRun {
		printFirstRunBanner(cfg.MCPToken, cfg.TokenFilePath)
	}

	// One API client shared across every incoming MCP session: this server
	// has a single identity (the account that minted the PAT), so there is
	// nothing session-specific to isolate.
	client := api.New(cfg.APIURL, cfg.AccessToken)

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "firebin",
		Version: version.BuildVersion,
	}, nil)
	tools.RegisterAll(mcpServer, client)
	resources.RegisterAll(mcpServer, client)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.Handle("/mcp", requireBearer(cfg.MCPToken, mcpHandler))

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("http server listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server crashed", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

// ─── handlers ────────────────────────────────────────────────────────────────

// healthHandler is deliberately unauthenticated: it is what the k8s probes
// and the uptime monitor hit, and it reveals nothing but the version.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"service": "firebin-mcp",
		"version": version.BuildVersion,
	})
}

// ─── middleware ──────────────────────────────────────────────────────────────

// requireBearer gates MCP traffic behind this server's inbound token. The
// comparison is constant-time so a response cannot be timed to recover the
// token byte by byte.
func requireBearer(token string, next http.Handler) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(got) == 0 || subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "missing or invalid authorization header",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── startup banner ──────────────────────────────────────────────────────────

// printFirstRunBanner dumps a visible block to stdout the first time the
// server generates its own inbound token. Printed once per install; later
// boots read the token back from disk.
func printFirstRunBanner(token, tokenFile string) {
	line := strings.Repeat("═", 63)
	fmt.Println()
	fmt.Printf("╔%s╗\n", line)
	fmt.Printf("║  FireBin MCP — first run                                      ║\n")
	fmt.Printf("║                                                               ║\n")
	fmt.Printf("║  A new MCP access token has been generated:                   ║\n")
	fmt.Printf("║                                                               ║\n")
	fmt.Printf("║    %-59s║\n", token)
	fmt.Printf("║                                                               ║\n")
	fmt.Printf("║  Stored at %-51s║\n", tokenFile)
	fmt.Printf("║  Set it in your MCP client config as                          ║\n")
	fmt.Printf("║    Authorization: Bearer <the token above>                    ║\n")
	fmt.Printf("║                                                               ║\n")
	fmt.Printf("║  This banner will not print again. To rotate, delete the      ║\n")
	fmt.Printf("║  token file and restart the container.                        ║\n")
	fmt.Printf("╚%s╝\n", line)
	fmt.Println()
}
