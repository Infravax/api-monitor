# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.25-alpine AS build
WORKDIR /src

# go.mod/go.sum are copied and downloaded before the rest of the source,
# so this layer only re-runs (and re-downloads modules) when dependencies
# actually change, not on every source edit.
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/api-monitor ./cmd/api-monitor

# ---- runtime ----
FROM alpine:3.20

# Alpine (not distroless) is used specifically so the HEALTHCHECK below can
# use busybox's wget — a real check, not a fake one, is the point.
#
# ca-certificates: the checker (M3) makes real outbound HTTPS requests to
# whatever targets are registered, and Go's TLS verification needs a
# trusted root store to check their certificates against — Alpine's base
# image doesn't include one by default. This was a latent gap since M3;
# fixed here while already touching this file for M6, not something M6
# itself required.
RUN apk add --no-cache ca-certificates \
    && addgroup -S apimonitor && adduser -S apimonitor -G apimonitor
COPY --from=build /out/api-monitor /usr/local/bin/api-monitor

USER apimonitor
EXPOSE 8080

# Assumes the default HTTP_ADDR (:8080). If HTTP_ADDR is overridden at run
# time, override this HEALTHCHECK's port to match.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/api-monitor"]
