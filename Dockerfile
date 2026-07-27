# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION is injected by the release workflow. Left unset for local builds,
# where internal/version computes "{YY}.{M}.DEV" at startup instead.
ARG VERSION
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w ${VERSION:+-X 'github.com/firelabsca/firebin-mcp/internal/version.Version=${VERSION}'}" \
    -o /out/firebin-mcp ./cmd/mcp

# Pre-create the state dir owned by the distroless nonroot user (65532), so
# the first-run token file can be written even with no volume mounted.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/firebin-mcp /app/firebin-mcp
COPY --from=build --chown=65532:65532 /out/data /data
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/app/firebin-mcp"]
