# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.25-alpine AS build
WORKDIR /src

# No third-party dependencies exist yet (see go.mod), so there is no
# go.sum / module cache step to add here. This copy is deliberately kept
# separate from the rest of the source so it stays cache-friendly once
# dependencies are introduced in a later milestone.
COPY go.mod ./
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/api-monitor ./cmd/api-monitor

# ---- runtime ----
FROM alpine:3.20

# Alpine (not distroless) is used specifically so the HEALTHCHECK below can
# use busybox's wget — a real check, not a fake one, is the point.
RUN addgroup -S apimonitor && adduser -S apimonitor -G apimonitor
COPY --from=build /out/api-monitor /usr/local/bin/api-monitor

USER apimonitor
EXPOSE 8080

# Assumes the default HTTP_ADDR (:8080). If HTTP_ADDR is overridden at run
# time, override this HEALTHCHECK's port to match.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/api-monitor"]
