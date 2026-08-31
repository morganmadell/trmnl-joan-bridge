#!/bin/bash
# deploy.sh — build (or pull) and (re)run the trmnl-joan-bridge container.
#
# Usage:
#   ./deploy.sh          Build the image locally from this directory's
#                         Dockerfile and run it. This is the correct default
#                         today: no ghcr.io image is published for this
#                         branch, only for pushes to main.
#   ./deploy.sh --pull    Skip the local build and instead pull
#                         ghcr.io/morganmadell/trmnl-joan-bridge:latest and
#                         run that. Documented future path: use this once
#                         this branch has merged to main and CI has
#                         published an image for it.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

IMAGE="trmnl-joan-bridge"
GHCR_IMAGE="ghcr.io/morganmadell/trmnl-joan-bridge:latest"
CONTAINER_NAME="trmnl-joan-bridge"
PULL_MODE=0

for arg in "$@"; do
  case "$arg" in
    --pull)
      PULL_MODE=1
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      echo "Usage: $0 [--pull]" >&2
      exit 1
      ;;
  esac
done

# --- Docker daemon check ---

if ! docker info >/dev/null 2>&1; then
  echo "Error: Docker daemon isn't running / accessible." >&2
  echo "Start Docker (or Docker Desktop / the WSL2 Docker service) and try again." >&2
  exit 1
fi

# --- .env setup ---

if [ ! -f .env ]; then
  cp .env.example .env
  echo "No .env found — created one from .env.example."
  echo "Edit .env with your real HA_URL / HA_TOKEN (and any other settings), then re-run ./deploy.sh."
  exit 1
fi

set -a
# shellcheck disable=SC1091
source .env
set +a

if [ -z "${SOURCE:-}" ] || [ "${SOURCE:-}" = "ha" ]; then
  if [ -z "${HA_URL:-}" ]; then
    echo "Error: HA_URL is not set in .env (required for SOURCE=ha)." >&2
    exit 1
  fi
  if [ -z "${HA_TOKEN:-}" ]; then
    echo "Error: HA_TOKEN is not set in .env (required for SOURCE=ha)." >&2
    exit 1
  fi
fi

# --- Build or pull the image ---

if [ "$PULL_MODE" -eq 1 ]; then
  echo "Pulling $GHCR_IMAGE ..."
  docker pull "$GHCR_IMAGE"
  RUN_IMAGE="$GHCR_IMAGE"
else
  echo "Building $IMAGE from local Dockerfile ..."
  docker build -t "$IMAGE" .
  RUN_IMAGE="$IMAGE"
fi

# --- Remove any existing container of the same name ---

EXISTING="$(docker ps -aq -f name=^${CONTAINER_NAME}$)"
if [ -n "$EXISTING" ]; then
  echo "Removing existing container $CONTAINER_NAME ..."
  docker rm -f "$CONTAINER_NAME"
fi

# --- Run ---

mkdir -p debug-screenshots

docker run -d \
  --name "$CONTAINER_NAME" \
  --restart unless-stopped \
  -p 11112:11112 \
  --env-file .env \
  -v "$(pwd)/zones.json:/app/zones.json" \
  -v "$(pwd)/debug-screenshots:/app/debug" \
  "$RUN_IMAGE"

echo
echo "Deployed: $CONTAINER_NAME is up (image: $RUN_IMAGE)."
echo "Watch startup with: docker logs -f $CONTAINER_NAME"
echo
echo "Known issue: this bridge occasionally exits with code 2 within seconds"
echo "of a real device connecting (root cause not yet found) — --restart"
echo "unless-stopped (already applied) recovers automatically within a few"
echo "seconds. See bridge/README.md's troubleshooting section if it keeps happening."

if grep -qi microsoft /proc/version 2>/dev/null; then
  echo
  echo "Running under WSL2 — see bridge/README.md's 'Running on Windows via WSL2' section for the networking setup this needs (mirrored networking mode + hostAddressLoopback + a Windows Firewall rule for port 11112)."
fi
