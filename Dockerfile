# syntax=docker/dockerfile:1

# ── build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY pv3/ ./pv3/

RUN CGO_ENABLED=0 go build -trimpath -o /trmnl-joan-bridge -ldflags="-s -w" .

# ── runtime stage ─────────────────────────────────────────────────────────────
# Fork note: the upstream project uses `scratch` here (no HA mode needs a
# browser). This fork's default content source (SOURCE=ha) screenshots a Home
# Assistant dashboard with a headless Chromium, so the runtime image needs an
# actual browser present — chromedp/headless-shell is a minimal, purpose-built
# headless-Chromium image (multi-arch: amd64 + arm64) that fits that need
# without pulling in a full desktop Chrome/Debian install.
FROM chromedp/headless-shell:latest

# ca-certificates for HTTPS calls to Home Assistant (if it's behind TLS) or
# TRMNL; not guaranteed present in the headless-shell base.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /trmnl-joan-bridge /trmnl-joan-bridge

# ZONES_FILE defaults to the relative path "zones.json", resolved against this
# working directory — mount your copy to /app/zones.json (see Instructions.md).
WORKDIR /app

# The base image's own ENTRYPOINT (/headless-shell/run.sh) launches Chromium
# itself as the container's main process — we don't want that; our Go binary
# is the main process here, and it shells out to the headless-shell binary
# (already on PATH) per-render instead.
ENTRYPOINT ["/trmnl-joan-bridge"]

EXPOSE 11112

ENV SOURCE=ha
ENV CHROMIUM_BIN=headless-shell

# Required (SOURCE=ha, the default): HA_URL, HA_TOKEN
# Optional (SOURCE=ha): ZONES_FILE (default: zones.json — mount your copy at
#   this path), RENDER_WAIT_MS (default: 4000), DEBUG_SAVE_SCREENSHOTS
# Required (SOURCE=trmnl): TRMNL_SERVER, DEVICE_ID, ACCESS_TOKEN
# Optional (either mode): REFRESH_INTERVAL (default 60s), LISTEN_ADDR (default :11112)
