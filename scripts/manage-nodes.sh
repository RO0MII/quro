#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Quro Node Manager — Pterodactyl-style CLI for node ops
# ============================================================
# Usage:
#   ./manage-nodes.sh add <name> <address> <port>
#   ./manage-nodes.sh list
#   ./manage-nodes.sh show <id|name>
#   ./manage-nodes.sh remove <id|name>
#   ./manage-nodes.sh token <id|name>        # regenerate token
#   ./manage-nodes.sh update <id> <field> <value>
#   ./manage-nodes.sh install <id>            # show install command
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/../.env"

# Default DB connection (overridden by .env)
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-quro}"
DB_PASSWORD="${DB_PASSWORD:-quro_secret}"
DB_NAME="${DB_NAME:-quro}"

# Source .env if available
if [ -f "$ENV_FILE" ]; then
  set -a
  source "$ENV_FILE"
  set +a
fi

export PGPASSWORD="$DB_PASSWORD"
PSQL=(psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -A -q)

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info()  { echo -e "${BLUE}  →${NC} $1"; }
log_ok()    { echo -e "${GREEN}  ✓${NC} $1"; }
log_warn()  { echo -e "${YELLOW}  ⚠${NC} $1"; }
log_err()   { echo -e "${RED}  ✖${NC} $1"; }

ensure_psql() {
  if ! command -v psql &>/dev/null; then
    log_err "psql is required. Install with: sudo apt install postgresql-client"
    exit 1
  fi
}

generate_token() {
  # 64-char hex token (same as Go's generateToken)
  openssl rand -hex 32 2>/dev/null || python3 -c "import secrets; print(secrets.token_hex(32))"
}

resolve_node() {
  local id_or_name="$1"
  local row
  # Try by UUID first, then by name
  row=$("${PSQL[@]}" -c "
    SELECT id, name, address, port, token, status,
           total_ram, used_ram, total_cpu, used_cpu, total_disk, used_disk,
           daemon_version, last_heartbeat, created_at
    FROM nodes WHERE id='$(echo "$id_or_name" | sed "s/'/''/g")'
    UNION ALL
    SELECT id, name, address, port, token, status,
           total_ram, used_ram, total_cpu, used_cpu, total_disk, used_disk,
           daemon_version, last_heartbeat, created_at
    FROM nodes WHERE name='$(echo "$id_or_name" | sed "s/'/''/g")'
    LIMIT 1
  " 2>/dev/null || true)

  if [ -z "$row" ]; then
    return 1
  fi
  echo "$row"
}

cmd_add() {
  local name="${1:-}"
  local address="${2:-}"
  local port="${3:-8081}"
  local panel_url="${4:-}"

  if [ -z "$name" ] || [ -z "$address" ]; then
    echo "Usage: $0 add <name> <address> [port] [panel_url]"
    echo ""
    echo "Examples:"
    echo "  $0 add node1 203.0.113.10 8081"
    echo "  $0 add node1 203.0.113.10 8081 https://panel.example.com"
    exit 1
  fi

  # Validate port
  if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
    log_err "Port must be between 1 and 65535"
    exit 1
  fi

  # Check for duplicate name
  local exists
  exists=$("${PSQL[@]}" -c "SELECT id FROM nodes WHERE name='$(echo "$name" | sed "s/'/''/g")' LIMIT 1" 2>/dev/null || true)
  if [ -n "$exists" ]; then
    log_err "Node with name '$name' already exists"
    exit 1
  fi

  local token
  token=$(generate_token)

  log_info "Creating node '$name' at $address:$port..."

  local result
  result=$("${PSQL[@]}" -c "
    INSERT INTO nodes (name, address, port, token)
    VALUES ('$(echo "$name" | sed "s/'/''/g")',
            '$(echo "$address" | sed "s/'/''/g")',
            $port,
            '$token')
    RETURNING id, name, address, port, token, status, created_at;
  " 2>/dev/null || true)

  if [ -z "$result" ]; then
    log_err "Failed to create node"
    exit 1
  fi

  # Parse the result: id|name|address|port|token|status|created_at
  local node_id node_name node_addr node_port node_token node_status node_created
  IFS='|' read -r node_id node_name node_addr node_port node_token node_status node_created <<< "$result"

  echo ""
  log_ok "Node created successfully!"
  echo ""
  echo -e "  ${CYAN}ID:${NC}      $node_id"
  echo -e "  ${CYAN}Name:${NC}    $node_name"
  echo -e "  ${CYAN}Address:${NC} $node_addr:$node_port"
  echo -e "  ${CYAN}Token:${NC}   $node_token"
  echo -e "  ${CYAN}Status:${NC}  $node_status"
  echo ""

  if [ -n "$panel_url" ]; then
    echo -e "  ${GREEN}── Install Command ───────────────────────────────${NC}"
    echo ""
    echo -e "  ${YELLOW}Run this on the node VPS as root:${NC}"
    echo ""
    echo -e "  curl -fsSL ${panel_url}/install-daemon.sh | bash -s -- \\"
    echo -e "    ${panel_url} ${node_token} ${node_id}"
    echo ""
  else
    echo -e "  ${YELLOW}── Install Command ───────────────────────────────${NC}"
    echo ""
    echo -e "  On the node VPS, create ${YELLOW}/etc/quro/wings.json${NC}:"
    echo ""
    cat <<JSONEOF
  cat > /etc/quro/wings.json <<'EOF'
  {
    "panel_url": "http://YOUR_PANEL_IP:8080",
    "node_id": "${node_id}",
    "token": "${node_token}",
    "node_name": "${node_name}",
    "port": ${port},
    "data_dir": "/var/lib/quro",
    "docker_host": "unix:///var/run/docker.sock",
    "version": "0.1.0"
  }
  EOF
JSONEOF
    echo ""
    echo -e "  Then run the daemon installer:"
    echo -e "  ${YELLOW}curl -fsSL https://raw.githubusercontent.com/quro/quro/main/install-daemon.sh | bash${NC}"
    echo ""
  fi
}

cmd_list() {
  local rows
  rows=$("${PSQL[@]}" -c "
    SELECT id, name, address, port, status, total_ram, used_ram,
           total_cpu, used_cpu, total_disk, used_disk, daemon_version,
           COALESCE(last_heartbeat::text, 'never'), created_at
    FROM nodes ORDER BY created_at DESC;
  " 2>/dev/null || true)

  if [ -z "$rows" ]; then
    log_info "No nodes found"
    return
  fi

  printf "\n  %-36s %-20s %-21s %-12s %-8s %s\n" "ID" "NAME" "ADDRESS" "STATUS" "VERSION" "HEARTBEAT"
  printf "  %-36s %-20s %-21s %-12s %-8s %s\n" "$(printf '%.0s─' {1..36})" "$(printf '%.0s─' {1..20})" "$(printf '%.0s─' {1..21})" "$(printf '%.0s─' {1..12})" "$(printf '%.0s─' {1..8})" "$(printf '%.0s─' {1..19})"

  while IFS='|' read -r id name address port status total_ram used_ram total_cpu used_cpu total_disk used_disk daemon_version last_heartbeat created_at; do
    [ -z "$id" ] && continue
    local status_color
    case "$status" in
      connected)    status_color="${GREEN}${status}${NC}" ;;
      disconnected) status_color="${RED}${status}${NC}" ;;
      *)            status_color="${YELLOW}${status}${NC}" ;;
    esac
    local heartbeat_display
    if [ "$last_heartbeat" = "never" ] || [ -z "$last_heartbeat" ]; then
      heartbeat_display="${RED}never${NC}"
    else
      heartbeat_display="$last_heartbeat"
    fi
    printf "  %-36s %-20s %-21s %b %-8s %b\n" "$id" "$name" "$address:$port" "$status_color" "$daemon_version" "$heartbeat_display"
  done <<< "$rows"
  echo ""
}

cmd_show() {
  local id_or_name="${1:-}"
  if [ -z "$id_or_name" ]; then
    echo "Usage: $0 show <id|name>"
    exit 1
  fi

  local row
  row=$(resolve_node "$id_or_name") || {
    log_err "Node not found: $id_or_name"
    exit 1
  }

  local id name address port token status total_ram used_ram total_cpu used_cpu total_disk used_disk daemon_version last_heartbeat created_at
  IFS='|' read -r id name address port token status total_ram used_ram total_cpu used_cpu total_disk used_disk daemon_version last_heartbeat created_at <<< "$row"

  local status_color
  case "$status" in
    connected)    status_color="${GREEN}${status}${NC}" ;;
    disconnected) status_color="${RED}${status}${NC}" ;;
    *)            status_color="${YELLOW}${status}${NC}" ;;
  esac

  echo ""
  echo -e "  ${CYAN}Node: ${name}${NC}"
  echo -e "  ${BLUE}──────────────────────────────────────────────${NC}"
  echo -e "  ID:          $id"
  echo -e "  Name:        $name"
  echo -e "  Address:     $address:$port"
  echo -e "  Token:       $token"
  echo -e "  Status:      $(eval echo $status_color)"
  echo -e "  Daemon ver:  $daemon_version"
  echo -e "  Resources:   RAM ${used_ram}/${total_ram}  CPU ${used_cpu}/${total_cpu}  Disk ${used_disk}/${total_disk}"
  if [ "$last_heartbeat" = "never" ] || [ -z "$last_heartbeat" ]; then
    echo -e "  Last hb:     ${RED}never${NC}"
  else
    echo -e "  Last hb:     $last_heartbeat"
  fi
  echo -e "  Created:     $created_at"
  echo ""
}

cmd_remove() {
  local id_or_name="${1:-}"
  if [ -z "$id_or_name" ]; then
    echo "Usage: $0 remove <id|name>"
    exit 1
  fi

  local row
  row=$(resolve_node "$id_or_name") || {
    log_err "Node not found: $id_or_name"
    exit 1
  }

  local id name
  IFS='|' read -r id name _ <<< "$row"

  log_warn "About to DELETE node '$name' ($id)"
  read -rp "  Are you sure? [y/N]: " confirm
  if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
    log_info "Cancelled"
    exit 0
  fi

  "${PSQL[@]}" -c "DELETE FROM nodes WHERE id='$id';" >/dev/null 2>&1
  log_ok "Node '$name' deleted"
}

cmd_token() {
  local id_or_name="${1:-}"
  if [ -z "$id_or_name" ]; then
    echo "Usage: $0 token <id|name>"
    exit 1
  fi

  local row
  row=$(resolve_node "$id_or_name") || {
    log_err "Node not found: $id_or_name"
    exit 1
  }

  local id name
  IFS='|' read -r id name _ <<< "$row"

  local new_token
  new_token=$(generate_token)

  "${PSQL[@]}" -c "
    UPDATE nodes SET token='$new_token', status='disconnected' WHERE id='$id';
  " >/dev/null 2>&1

  log_ok "Token regenerated for node '$name'"
  echo -e "  New token: ${YELLOW}$new_token${NC}"
  echo ""
  echo -e "  Update ${YELLOW}/etc/quro/wings.json${NC} on the node with the new token and restart the daemon."
}

cmd_update() {
  local id_or_name="${1:-}"
  local field="${2:-}"
  local value="${3:-}"

  if [ -z "$id_or_name" ] || [ -z "$field" ] || [ -z "$value" ]; then
    echo "Usage: $0 update <id|name> <field> <value>"
    echo "  Fields: name, address, port"
    exit 1
  fi

  local row
  row=$(resolve_node "$id_or_name") || {
    log_err "Node not found: $id_or_name"
    exit 1
  }

  local id name
  IFS='|' read -r id name _ <<< "$row"

  case "$field" in
    name)
      "${PSQL[@]}" -c "UPDATE nodes SET name='$(echo "$value" | sed "s/'/''/g")' WHERE id='$id';" >/dev/null 2>&1
      ;;
    address)
      "${PSQL[@]}" -c "UPDATE nodes SET address='$(echo "$value" | sed "s/'/''/g")' WHERE id='$id';" >/dev/null 2>&1
      ;;
    port)
      "${PSQL[@]}" -c "UPDATE nodes SET port=$value WHERE id='$id';" >/dev/null 2>&1
      ;;
    *)
      log_err "Unknown field: $field (use: name, address, port)"
      exit 1
      ;;
  esac

  log_ok "Node '$name' $field updated to '$value'"
}

cmd_install() {
  local id_or_name="${1:-}"
  local panel_url="${2:-}"

  if [ -z "$id_or_name" ]; then
    echo "Usage: $0 install <id|name> [panel_url]"
    echo "  panel_url defaults to https://your-server.com"
    echo ""
    echo "Example:"
    echo "  $0 install node1 https://panel.example.com"
    exit 1
  fi

  local row
  row=$(resolve_node "$id_or_name") || {
    log_err "Node not found: $id_or_name"
    exit 1
  }

  local id name address port token
  IFS='|' read -r id name address port token _ <<< "$row"

  if [ -z "$panel_url" ]; then
    # Try to detect panel_url from nginx config
    panel_url=$(grep -r "server_name" /etc/nginx/sites-enabled/ 2>/dev/null | head -1 | awk '{print $2}' | tr -d ';' || true)
    if [ -z "$panel_url" ]; then
      panel_url="http://YOUR_PANEL_IP:8080"
    else
      panel_url="https://$panel_url"
    fi
  fi

  echo ""
  echo -e "  ${GREEN}── Install Node: ${name} ───────────────────────────${NC}"
  echo ""
  echo -e "  ${YELLOW}Run this command on the node VPS as root:${NC}"
  echo ""
  echo -e "  ${CYAN}curl -fsSL ${panel_url}/install-daemon.sh | bash -s -- \\"
  echo -e "    ${panel_url} ${token} ${id}${NC}"
  echo ""
  echo -e "  ${YELLOW}Or manually with the config file approach:${NC}"
  echo ""
  echo -e "  ${CYAN}# Create config on the node VPS${NC}"
  echo -e "  ${CYAN}mkdir -p /etc/quro${NC}"
  echo ""
  cat <<JSONEOF
  cat > /etc/quro/wings.json <<'EOF'
  {
    "panel_url": "${panel_url}",
    "node_id": "${id}",
    "token": "${token}",
    "node_name": "${name}",
    "port": ${port},
    "data_dir": "/var/lib/quro",
    "docker_host": "unix:///var/run/docker.sock",
    "version": "0.1.0"
  }
  EOF
JSONEOF
  echo ""
  echo -e "  ${CYAN}# Then install Docker and run the daemon${NC}"
  echo -e "  ${CYAN}curl -fsSL https://get.docker.com | sh${NC}"
  echo -e "  ${CYAN}(run the daemon binary or use Docker Compose)${NC}"
  echo ""
}

cmd_help() {
  cat <<EOF
Quro Node Manager — Pterodactyl-style CLI

Usage:
  $0 add <name> <address> [port] [panel_url]    Create a new node
  $0 list                                         List all nodes
  $0 show <id|name>                               Show node details
  $0 remove <id|name>                             Delete a node
  $0 token <id|name>                              Regenerate node token
  $0 update <id> <field> <value>                  Update node field
  $0 install <id|name> [panel_url]                Show install command

Examples:
  $0 add node-1 203.0.113.10 8081 https://panel.quro.io
  $0 list
  $0 show node-1
  $0 token node-1
  $0 update node-1 address 203.0.113.20
  $0 remove node-1
EOF
}

main() {
  local cmd="${1:-help}"
  shift || true

  case "$cmd" in
    add)     ensure_psql; cmd_add "$@" ;;
    list)    ensure_psql; cmd_list ;;
    show)    ensure_psql; cmd_show "$@" ;;
    remove)  ensure_psql; cmd_remove "$@" ;;
    token)   ensure_psql; cmd_token "$@" ;;
    update)  ensure_psql; cmd_update "$@" ;;
    install) cmd_install "$@" ;;
    help|--help|-h) cmd_help ;;
    *)
      log_err "Unknown command: $cmd"
      echo "Run '$0 help' for usage."
      exit 1
      ;;
  esac
}

main "$@"
