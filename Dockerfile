# ─── build ──────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS build

WORKDIR /src

# Copy module files first so dependency downloads are cached
# across source changes.
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build API server
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server


# ─── runtime ────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget && \
    adduser -D -u 10001 cfn

WORKDIR /app

COPY --from=build /out/server /app/server

USER cfn

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/health || exit 1

ENTRYPOINT ["/app/server"]