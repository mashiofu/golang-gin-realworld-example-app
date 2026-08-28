# syntax=docker/dockerfile:1

## ---- Build stage ----
FROM golang:1.25-alpine AS build

# mattn/go-sqlite3 (kept for the local-dev SQLite fallback in
# common/database.go) needs cgo + a C toolchain to compile, even though the
# production path below always sets DATABASE_URL and uses the pure-Go
# postgres driver instead.
RUN apk add --no-cache gcc musl-dev

WORKDIR /src

# Copy go.mod/go.sum first so dependency downloads are cached in their own
# layer and only re-run when dependencies actually change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/conduit-api .

## ---- Runtime stage ----
FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates wget && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=build /out/conduit-api /app/conduit-api

# Writable by `app` for the SQLite fallback path in common/database.go
# (DATABASE_URL unset) - real deployments always set DATABASE_URL and
# never touch this, but the zero-config fallback should actually work
# when someone runs this image directly, not just via `go run` on a host.
RUN mkdir -p /app/data && chown -R app:app /app/data

USER app
EXPOSE 8080

# -O /dev/null (a real GET, discarded) rather than --spider: --spider sends
# a HEAD request, and Gin doesn't auto-alias HEAD to a GET-only route.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget --no-verbose --tries=1 -O /dev/null http://localhost:8080/api/ping/ || exit 1

ENTRYPOINT ["/app/conduit-api"]
