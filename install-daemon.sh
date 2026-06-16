#!/usr/bin/env bash
set -e

# ============================================================
# Quro Daemon (Wings) Installer
# ============================================================
# Usage:
#   curl -fsSL https://panel.quro.io/install-daemon.sh | bash -s -- <PANEL_URL> <NODE_TOKEN> <NODE_ID>
#
# Or with existing config:
#   # First create /etc/quro/wings.json, then:
#   curl -fsSL https://panel.quro.io/install-daemon.sh | bash
# ============================================================

PANEL_URL="${1:-}"
NODE_TOKEN="${2:-}"
NODE_ID="${3:-}"
DAEMON_DIR="/opt/quro-daemon"
CONFIG_DIR="/etc/quro"
CONFIG_FILE="$CONFIG_DIR/wings.json"
DAEMON_VERSION="0.1.0"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

print_banner() {
  echo ""
  echo -e "${BLUE}  ╔══════════════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}  ║            Quro Daemon Installer                 ║${NC}"
  echo -e "${BLUE}  ║         Pterodactyl-style Node Setup             ║${NC}"
  echo -e "${BLUE}  ╚══════════════════════════════════════════════════╝${NC}"
  echo ""
}

log_info()  { echo -e "${BLUE}  →${NC} $1"; }
log_ok()    { echo -e "${GREEN}  ✓${NC} $1"; }
log_warn()  { echo -e "${YELLOW}  ⚠${NC} $1"; }
log_err()   { echo -e "${RED}  ✖${NC} $1"; }

require_root() {
  if [ "$EUID" -ne 0 ]; then
    log_err "Please run as root (use sudo)."
    exit 1
  fi
}

ask() {
  local prompt="$1"
  local input
  read -rp "  $prompt: " input
  echo "$input"
}

check_docker() {
  if command -v docker &>/dev/null; then
    log_ok "Docker already installed"
    return 0
  fi
  return 1
}

install_docker() {
  log_info "Installing Docker..."
  curl -fsSL https://get.docker.com | sh
  systemctl enable docker
  systemctl start docker
  log_ok "Docker installed"
}

fetch_config_from_panel() {
  local panel_url="$1"
  local token="$2"
  local node_id="$3"

  log_info "Fetching node config from panel..."
  local json
  json=$(curl -fsSL "${panel_url}/api/nodes/${node_id}/config" \
    -H "Authorization: Bearer placeholder" \
    -H "X-Node-Token: ${token}" 2>/dev/null || echo "")

  if [ -z "$json" ]; then
    log_warn "Could not fetch config from panel (panel may require auth)."
    log_info "Will create config manually with provided values."
    return 1
  fi

  echo "$json" > "$CONFIG_FILE"
  chmod 600 "$CONFIG_FILE"
  log_ok "Config fetched from panel"
  return 0
}

write_config() {
  local panel_url="$1"
  local token="$2"
  local node_id="$3"

  log_info "Writing daemon config to $CONFIG_FILE..."
  mkdir -p "$CONFIG_DIR"

  # Fetch from panel first if we have all required params
  if [ -n "$panel_url" ] && [ -n "$token" ] && [ -n "$node_id" ]; then
    if fetch_config_from_panel "$panel_url" "$token" "$node_id"; then
      # Panel returned config — check if it needs the version field
      if ! grep -q '"version"' "$CONFIG_FILE" 2>/dev/null; then
        # Add version field to the config
        local tmp
        tmp=$(python3 -c "
import json, sys
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
cfg['version'] = '$DAEMON_VERSION'
json.dump(cfg, sys.stdout, indent=2)
" 2>/dev/null) && echo "$tmp" > "$CONFIG_FILE"
      fi
      return
    fi
  fi

  # Fallback: create config manually
  if [ -f "$CONFIG_FILE" ]; then
    log_info "Config file already exists at $CONFIG_FILE — preserving it."
    log_info "To overwrite, remove the file first: rm $CONFIG_FILE"
    return
  fi

  if [ -z "$panel_url" ] || [ -z "$token" ] || [ -z "$node_id" ]; then
    log_err "Missing required parameters: PANEL_URL, NODE_TOKEN, NODE_ID"
    log_err "Either provide them as arguments or create $CONFIG_FILE manually."
    exit 1
  fi

  cat > "$CONFIG_FILE" <<EOF
{
  "panel_url": "$panel_url",
  "node_id": "$node_id",
  "token": "$token",
  "node_name": "$(hostname)",
  "port": 8081,
  "data_dir": "/var/lib/quro",
  "docker_host": "unix:///var/run/docker.sock",
  "version": "$DAEMON_VERSION"
}
EOF
  chmod 600 "$CONFIG_FILE"
  log_ok "Config file created"
}

build_or_download_daemon() {
  log_info "Installing Quro daemon to $DAEMON_DIR..."
  rm -rf "$DAEMON_DIR"
  mkdir -p "$DAEMON_DIR"
  cd "$DAEMON_DIR"

  if command -v git &>/dev/null && command -v go &>/dev/null; then
    log_info "Building daemon from source..."
    git clone --depth 1 https://github.com/quro/quro.git "$DAEMON_DIR/src" 2>/dev/null || {
      log_warn "Git clone failed, trying download..."
      download_daemon
      return
    }
    cd "$DAEMON_DIR/src/daemon"
    go build -ldflags="-X main.Version=$DAEMON_VERSION" -o "$DAEMON_DIR/quro-daemon" ./cmd/daemon
    log_ok "Daemon built from source"
  else
    download_daemon
  fi
  chmod +x "$DAEMON_DIR/quro-daemon"
}

download_daemon() {
  log_info "Downloading pre-built daemon..."
  local panel_url
  panel_url=$(grep '"panel_url"' "$CONFIG_FILE" 2>/dev/null | head -1 | sed 's/.*"panel_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || echo "")

  if [ -n "$panel_url" ]; then
    curl -fsSL "${panel_url}/install/quro-daemon" -o "$DAEMON_DIR/quro-daemon" && return
  fi

  log_err "Could not download daemon binary."
  log_err "Install Go and run again, or build manually:"
  log_err "  cd /opt/quro-daemon && git clone https://github.com/quro/quro.git src"
  log_err "  cd src/daemon && go build -o /opt/quro-daemon/quro-daemon ./cmd/daemon"
  exit 1
}

install_systemd_service() {
  log_info "Installing systemd service..."
  cat > /etc/systemd/system/quro-daemon.service <<EOF
[Unit]
Description=Quro Daemon (Wings)
After=docker.service network.target
Requires=docker.service

[Service]
Type=simple
User=root
WorkingDirectory=$DAEMON_DIR
ExecStart=$DAEMON_DIR/quro-daemon
Restart=always
RestartSec=5
Environment=CONFIG_FILE=$CONFIG_FILE

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable quro-daemon
  systemctl restart quro-daemon
  log_ok "Systemd service installed and started"
}

wait_for_daemon() {
  log_info "Waiting for daemon to start..."
  local attempts=0
  while [ $attempts -lt 15 ]; do
    if systemctl is-active --quiet quro-daemon 2>/dev/null; then
      sleep 2
      log_ok "Daemon is running"
      return
    fi
    sleep 1
    attempts=$((attempts + 1))
  done
  log_warn "Daemon may not have started. Check logs: journalctl -u quro-daemon -f"
}

print_summary() {
  local node_id
  node_id=$(grep '"node_id"' "$CONFIG_FILE" 2>/dev/null | head -1 | sed 's/.*"node_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || echo "unknown")

  echo ""
  echo -e "${GREEN}  ═══════════════════════════════════════════════════${NC}"
  echo -e "${GREEN}  ✓ Quro Daemon installed successfully${NC}"
  echo ""
  echo -e "  Config:    ${CYAN}$CONFIG_FILE${NC}"
  echo -e "  Binary:    ${CYAN}$DAEMON_DIR/quro-daemon${NC}"
  echo -e "  Node ID:   ${CYAN}$node_id${NC}"
  echo ""
  echo -e "  ${YELLOW}Useful commands:${NC}"
  echo -e "    ${CYAN}systemctl status quro-daemon${NC}"
  echo -e "    ${CYAN}journalctl -u quro-daemon -f${NC}"
  echo -e "    ${CYAN}$DAEMON_DIR/quro-daemon --help${NC}"
  echo ""
  echo -e "  ${YELLOW}If the panel shows 'Disconnected', wait up to 30s${NC}"
  echo -e "  ${YELLOW}for the first heartbeat to be sent.${NC}"
  echo ""
}

main() {
  print_banner
  require_root

  # Accept args with or without config file
  if [ -n "$PANEL_URL" ] && [ -n "$NODE_TOKEN" ] && [ -n "$NODE_ID" ]; then
    :
  elif [ -f "$CONFIG_FILE" ]; then
    log_info "Found existing config at $CONFIG_FILE"
    log_info "Using config file values (ignoring any arguments)."
    # Try to extract values from existing config
    local existing_panel existing_token existing_id
    existing_panel=$(grep '"panel_url"' "$CONFIG_FILE" | head -1 | sed 's/.*"panel_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
    existing_token=$(grep '"token"' "$CONFIG_FILE" | head -1 | sed 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
    existing_id=$(grep '"node_id"' "$CONFIG_FILE" | head -1 | sed 's/.*"node_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
    PANEL_URL="${existing_panel:-$PANEL_URL}"
    NODE_TOKEN="${existing_token:-$NODE_TOKEN}"
    NODE_ID="${existing_id:-$NODE_ID}"
  else
    echo ""
    echo -e "  ${YELLOW}No config file or arguments found.${NC}"
    echo ""

    if [ -z "$PANEL_URL" ]; then
      PANEL_URL=$(ask "Panel URL (e.g. https://panel.quro.io)")
    fi
    if [ -z "$NODE_TOKEN" ]; then
      echo -e "  ${YELLOW}Create a node first in the panel, or use manage-nodes.sh:${NC}"
      echo -e "  ${YELLOW}  ./scripts/manage-nodes.sh add mynode <IP> 8081${NC}"
      echo ""
      NODE_TOKEN=$(ask "Node Token (from panel)")
    fi
    if [ -z "$NODE_ID" ]; then
      NODE_ID=$(ask "Node ID (from panel)")
    fi
  fi

  # Install Docker if not present
  if ! check_docker; then
    install_docker
  fi

  # Write config (fetches from panel if possible, otherwise manual)
  write_config "$PANEL_URL" "$NODE_TOKEN" "$NODE_ID"

  # Build or download the daemon
  build_or_download_daemon

  # Install systemd service
  install_systemd_service

  # Wait for daemon to be ready
  wait_for_daemon

  print_summary
}

main "$@"
