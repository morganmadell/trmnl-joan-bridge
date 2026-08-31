.PHONY: build build-arm run-ha run-trmnl

GO_IMAGE := golang:1.26-alpine

# Build for this machine (dev/test)
build:
	docker run --rm \
	  --mount type=bind,source=$(CURDIR),target=/src -w /src \
	  -e CGO_ENABLED=0 \
	  $(GO_IMAGE) go build -o bin/trmnl-joan-bridge-local .

# Build ARM64 static binary for Raspberry Pi deployment
build-arm:
	docker run --rm \
	  --mount type=bind,source=$(CURDIR),target=/src -w /src \
	  -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=arm64 \
	  $(GO_IMAGE) go build -o bin/trmnl-joan-bridge .

# Run against Home Assistant (default source; set HA_URL, HA_TOKEN, and put a
# zones.json next to the binary — see zones.example.json). Needs `chromium` or
# `headless-shell` on PATH inside whatever you run this in; the plain Go dev
# image above does NOT have one — use `docker build .` (Dockerfile) instead
# for anything beyond compiling/testing.
run-ha: build
	docker run --rm --name trmnl-joan-bridge \
	  --mount type=bind,source=$(CURDIR),target=/app -w /app \
	  -p 11112:11112 \
	  -e SOURCE=ha -e HA_URL="$(HA_URL)" -e HA_TOKEN="$(HA_TOKEN)" \
	  $(GO_IMAGE) ./bin/trmnl-joan-bridge-local

# Run via TRMNL (set TRMNL_SERVER, DEVICE_ID, ACCESS_TOKEN)
run-trmnl: build
	docker run --rm --name trmnl-joan-bridge \
	  --mount type=bind,source=$(CURDIR),target=/app -w /app \
	  -p 11112:11112 \
	  $(GO_IMAGE) ./bin/trmnl-joan-bridge-local \
	    -source trmnl \
	    -trmnl-server "$(TRMNL_SERVER)" \
	    -device-id "$(DEVICE_ID)" \
	    -access-token "$(ACCESS_TOKEN)" \
	    -refresh "$(or $(REFRESH),60s)"

