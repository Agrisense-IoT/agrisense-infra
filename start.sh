#!/usr/bin/env bash
# =============================================================================
# start.sh — Agrisense full-stack launcher
#
# Usage:
#   chmod +x start.sh
#   ./start.sh           [--build] [--down] [--no-frontend] [--help]
#
# Options:
#   --build          Force a rebuild of Docker images before starting
#   --down           Tear down the stack instead of starting it
#   --no-frontend    Start without the frontend service
#   --help           Show this help message
# =============================================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

info()    { echo -e "${CYAN}[agrisense]${RESET} $*"; }
success() { echo -e "${GREEN}[agrisense]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[agrisense]${RESET} $*"; }
error()   { echo -e "${RED}[agrisense]${RESET} $*" >&2; }

BUILD=false
TEARDOWN=false
NO_FRONTEND=false

for arg in "$@"; do
  case "$arg" in
    --build)        BUILD=true ;;
    --down)         TEARDOWN=true ;;
    --no-frontend)  NO_FRONTEND=true ;;
    --help)
      grep '^#' "$0" | sed 's/^# \{0,\}//'
      exit 0
      ;;
    *)
      error "Unknown option: $arg  (use --help to see available options)"
      exit 1
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

ENV_FILE="./.env"

echo ""
echo -e "${BOLD}  Agrisense Full-Stack Launcher${RESET}"
echo ""

# ── Pre-flight checks ──────────────────────────────────────────────────────
if ! docker info > /dev/null 2>&1; then
  error "Docker is not running. Please start Docker Desktop and try again."
  exit 1
fi
success "Docker is running."

if [ ! -f "$ENV_FILE" ]; then
  error "Environment file not found: $ENV_FILE"
  error "Copy .env.example to .env and configure it."
  exit 1
fi
success "Environment file found: $ENV_FILE"

# Check sibling repos exist
if [ ! -d "../agrisense-backend" ]; then
  error "Backend repo not found at ../agrisense-backend"
  error "Clone it: git clone git@github.com:Agrisense-IoT/agrisense-backend.git ../agrisense-backend"
  exit 1
fi

if [ "$NO_FRONTEND" = false ] && [ ! -d "../agrisense-frontend" ]; then
  warn "Frontend repo not found at ../agrisense-frontend — starting without frontend."
  NO_FRONTEND=true
fi

# ── Network setup ──────────────────────────────────────────────────────────
NETWORK_NAME="agrisense_internal"
PARSED=$(grep -E '^DOCKER_NETWORK_NAME=' "$ENV_FILE" | cut -d'=' -f2 | tr -d '"' | tr -d "'" || true)
[ -n "$PARSED" ] && NETWORK_NAME="$PARSED"
info "Docker network: ${BOLD}${NETWORK_NAME}${RESET}"

if docker network inspect "$NETWORK_NAME" > /dev/null 2>&1; then
  success "Network '${NETWORK_NAME}' already exists."
else
  info "Creating Docker network '${NETWORK_NAME}'..."
  docker network create "$NETWORK_NAME"
  success "Network '${NETWORK_NAME}' created."
fi

# ── Tear-down mode ─────────────────────────────────────────────────────────
if [ "$TEARDOWN" = true ]; then
  info "Tearing down the stack..."
  docker compose down
  success "Stack stopped."
  exit 0
fi

# ── Compose profiles ───────────────────────────────────────────────────────
COMPOSE_CMD="docker compose"
if [ "$NO_FRONTEND" = true ]; then
  COMPOSE_CMD="$COMPOSE_CMD --scale frontend=0"
fi

if [ "$BUILD" = true ]; then
  info "Building images and starting stack..."
  $COMPOSE_CMD up -d --build
else
  info "Starting stack (use --build to force image rebuild)..."
  $COMPOSE_CMD up -d
fi

echo ""
success "Stack is up!"
echo ""

# ── Print service URLs ─────────────────────────────────────────────────────
PORT=$(grep -E '^PORT=' "$ENV_FILE" | cut -d'=' -f2 | tr -d '"' | tr -d "'" || true)
FRONTEND_PORT=$(grep -E '^FRONTEND_PORT=' "$ENV_FILE" | cut -d'=' -f2 | tr -d '"' | tr -d "'" || true)
SUPABASE_URL=$(grep -E '^SUPABASE_PUBLIC_URL=' "$ENV_FILE" | cut -d'=' -f2 | tr -d '"' | tr -d "'" || true)

echo -e "  ${BOLD}Frontend       ${RESET}  http://localhost:${FRONTEND_PORT:-3000}"
echo -e "  ${BOLD}Agrisense API  ${RESET}  http://localhost:${PORT:-3143}"
echo -e "  ${BOLD}API Docs       ${RESET}  http://localhost:${PORT:-3143}/api"
echo -e "  ${BOLD}Supabase Studio${RESET}  ${SUPABASE_URL:-http://localhost:8000}"
echo -e "  ${BOLD}MQTT Broker    ${RESET}  localhost:1883"
echo ""
echo -e "  ${CYAN}Tip:${RESET} Run ${BOLD}docker compose logs -f api${RESET} to follow API logs."
echo -e "  ${CYAN}Tip:${RESET} Run ${BOLD}./start.sh --down${RESET} to stop the stack."
echo ""
