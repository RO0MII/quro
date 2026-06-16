#!/usr/bin/env bash
set -e

# ============================================================
# Quro Panel Production Installer
# With Nginx Reverse Proxy + Let's Encrypt SSL
# ============================================================
# Usage: curl -fsSL https://your-domain.com/install.sh | bash
#        or: sudo bash install.sh
# ============================================================

REPO_URL="https://github.com/quro/quro.git"
INSTALL_DIR="/opt/quro"
DOMAIN=""
ADMIN_EMAIL=""
ADMIN_PASSWORD=""
ADMIN_USERNAME="admin"
USE_SSL="yes"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_banner() {
    echo ""
    echo -e "${BLUE}  ╔══════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}  ║              Quro Panel Installer                ║${NC}"
    echo -e "${BLUE}  ║         Nginx + SSL + Docker Compose           ║${NC}"
    echo -e "${BLUE}  ╚══════════════════════════════════════════════════╝${NC}"
    echo ""
}

log_info() { echo -e "${BLUE}  →${NC} $1"; }
log_ok() { echo -e "${GREEN}  ✓${NC} $1"; }
log_warn() { echo -e "${YELLOW}  ⚠${NC} $1"; }
log_err() { echo -e "${RED}  ✖${NC} $1"; }

ask() {
    local prompt="$1"
    local default="$2"
    local input
    if [ -n "$default" ]; then
        read -rp "  $prompt [$default]: " input
        echo "${input:-$default}"
    else
        read -rp "  $prompt: " input
        echo "$input"
    fi
}

ask_secret() {
    local prompt="$1"
    local input
    read -rsp "  $prompt: " input
    echo ""
    echo "$input"
}

ask_secret_confirm() {
    local prompt="$1"
    local pass1 pass2
    while true; do
        read -rsp "  $prompt: " pass1
        echo ""
        read -rsp "  Confirm password: " pass2
        echo ""
        if [ "$pass1" != "$pass2" ]; then
            log_err "Passwords do not match. Try again."
        else
            echo "$pass1"
            return
        fi
    done
}

require_root() {
    if [ "$EUID" -ne 0 ]; then
        log_err "Please run as root (use sudo)."
        exit 1
    fi
}

check_os() {
    if [[ ! -f /etc/os-release ]]; then
        log_err "Cannot detect OS. Ubuntu recommended."
        exit 1
    fi
    source /etc/os-release
    if [[ "$ID" != "ubuntu" && "$ID" != "debian" ]]; then
        log_warn "This script is optimized for Ubuntu/Debian. Continue at your own risk."
        sleep 2
    fi
}

install_packages() {
    log_info "Updating system packages..."
    apt-get update -y
    apt-get upgrade -y

    log_info "Installing required packages..."
    apt-get install -y \
        curl \
        wget \
        git \
        nginx \
        certbot \
        python3-certbot-nginx \
        software-properties-common \
        apt-transport-https \
        ca-certificates \
        gnupg2 \
        ufw \
        jq
}

install_docker() {
    if command -v docker &>/dev/null && docker compose version &>/dev/null; then
        log_ok "Docker and Docker Compose already installed"
        return
    fi

    log_info "Installing Docker..."
    curl -fsSL https://get.docker.com | sh
    systemctl enable docker
    systemctl start docker

    if ! docker compose version &>/dev/null; then
        log_info "Installing Docker Compose plugin..."
        apt-get install -y docker-compose-plugin
    fi

    log_ok "Docker installed"
}

clone_repo() {
    if [ -d "$INSTALL_DIR" ]; then
        log_ok "Quro already installed at $INSTALL_DIR"
        log_info "Pulling latest changes..."
        cd "$INSTALL_DIR"
        git pull --quiet || true
    else
        log_info "Cloning Quro into $INSTALL_DIR..."
        git clone --depth 1 "$REPO_URL" "$INSTALL_DIR"
        cd "$INSTALL_DIR"
    fi
}

collect_inputs() {
    echo ""
    echo -e "${BLUE}  ── Panel Configuration ────────────────────────────${NC}"
    echo ""

    DOMAIN=$(ask "Domain (e.g. panel.example.com)")
    while [ -z "$DOMAIN" ]; do
        log_err "Domain is required"
        DOMAIN=$(ask "Domain (e.g. panel.example.com)")
    done

    ADMIN_EMAIL=$(ask "Admin email")
    while [ -z "$ADMIN_EMAIL" ] || [[ ! "$ADMIN_EMAIL" =~ @ ]]; do
        log_err "Valid email is required"
        ADMIN_EMAIL=$(ask "Admin email")
    done

    ADMIN_PASSWORD=$(ask_secret_confirm "Admin password (min 8 chars)")
    while [ "${#ADMIN_PASSWORD}" -lt 8 ]; do
        log_err "Password must be at least 8 characters"
        ADMIN_PASSWORD=$(ask_secret_confirm "Admin password (min 8 chars)")
    done

    # Ask about SSL
    local ssl_input
    read -rp "  Enable Let's Encrypt SSL? [Y/n]: " ssl_input
    ssl_input=${ssl_input:-Y}
    if [[ "$ssl_input" =~ ^[Nn] ]]; then
        USE_SSL="no"
    fi

    echo ""
}

write_env() {
    local api_url="http://localhost:8080"
    local ws_url="ws://localhost:8080"

    # For internal Docker communication
    cat > "$INSTALL_DIR/.env" <<EOF
# Quro Panel environment
DOMAIN=$DOMAIN
ADMIN_EMAIL=$ADMIN_EMAIL
ADMIN_PASSWORD=$ADMIN_PASSWORD

# Database
DB_HOST=postgres
DB_PORT=5432
DB_USER=quro
DB_PASSWORD=quro_secret
DB_NAME=quro

# Redis
REDIS_ADDR=redis:6379

# API / Panel
JWT_SECRET=$(openssl rand -hex 32)
PANEL_URL=$api_url
NEXT_PUBLIC_API_URL=$api_url
NEXT_PUBLIC_WS_URL=$ws_url
EOF

    log_ok "Environment file written to $INSTALL_DIR/.env"
}

configure_nginx() {
    log_info "Configuring Nginx..."

    # Remove default site
    rm -f /etc/nginx/sites-enabled/default

    # Create upstream config
    cat > /etc/nginx/sites-available/quro-panel.conf <<EOF
upstream quro_panel_ui {
    server 127.0.0.1:3000;
}

upstream quro_panel_api {
    server 127.0.0.1:8080;
}

server {
    listen 80;
    listen [::]:80;
    server_name ${DOMAIN};

    client_max_body_size 100M;

    location / {
        proxy_pass http://quro_panel_ui;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_cache_bypass \$http_upgrade;
    }

    location /api/ {
        proxy_pass http://quro_panel_api/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location /ws {
        proxy_pass http://quro_panel_api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

    ln -sf /etc/nginx/sites-available/quro-panel.conf /etc/nginx/sites-enabled/quro-panel.conf

    nginx -t
    systemctl restart nginx
    systemctl enable nginx

    log_ok "Nginx configured"
}

configure_ssl() {
    if [ "$USE_SSL" != "yes" ]; then
        log_warn "SSL disabled. Running on HTTP only."
        return
    fi

    log_info "Obtaining Let's Encrypt SSL certificate..."

    # Check if certbot can obtain certificate
    if certbot --nginx --agree-tos --no-eff-email --email "$ADMIN_EMAIL" -d "$DOMAIN" --non-interactive; then
        log_ok "SSL certificate installed"
    else
        log_warn "SSL certificate failed. Check DNS points to this server."
        log_warn "You can retry later with: certbot --nginx -d $DOMAIN"
    fi

    # Auto-renewal cron
    (crontab -l 2>/dev/null || true; echo "0 3 * * * certbot renew --quiet --nginx") | crontab -
}

configure_firewall() {
    log_info "Configuring firewall..."

    ufw default deny incoming
    ufw default allow outgoing
    ufw allow 22/tcp
    ufw allow 80/tcp
    ufw allow 443/tcp

    # Optional: block direct access to 3000 and 8080 in production
    # ufw deny 3000/tcp
    # ufw deny 8080/tcp

    ufw --force enable
    log_ok "Firewall configured"
}

start_services() {
    log_info "Starting Quro services with Docker Compose..."
    cd "$INSTALL_DIR"

    docker compose down --remove-orphans 2>/dev/null || true
    docker compose up -d --build

    log_ok "Services started"
}

wait_for_api() {
    log_info "Waiting for API to be ready..."
    local attempts=0
    while [ $attempts -lt 30 ]; do
        if curl -fsSL "http://localhost:8080/health" &>/dev/null; then
            log_ok "API is ready"
            return
        fi
        sleep 2
        attempts=$((attempts + 1))
    done
    log_warn "API did not become ready in time. Check logs with: docker compose logs -f panel-api"
}

install_scripts() {
    log_info "Installing public installer scripts..."
    mkdir -p "$INSTALL_DIR/public"
    cp "$INSTALL_DIR/install.sh" "$INSTALL_DIR/public/install.sh" 2>/dev/null || true
    cp "$INSTALL_DIR/install-daemon.sh" "$INSTALL_DIR/public/install-daemon.sh" 2>/dev/null || true
}

save_credentials() {
    cat > "$INSTALL_DIR/.credentials.txt" <<EOF
Quro Panel Credentials
======================
Panel URL:    https://${DOMAIN}  (or http://${DOMAIN} if SSL failed)
API URL:      https://${DOMAIN}/api
Admin Email:  ${ADMIN_EMAIL}
Admin User:   ${ADMIN_USERNAME}
Admin Pass:   ${ADMIN_PASSWORD}
Install Dir:  ${INSTALL_DIR}
SSL Enabled:  ${USE_SSL}
EOF
    chmod 600 "$INSTALL_DIR/.credentials.txt"
    log_ok "Credentials saved to $INSTALL_DIR/.credentials.txt"
}

print_summary() {
    local protocol="https"
    [ "$USE_SSL" != "yes" ] && protocol="http"

    echo ""
    echo -e "${GREEN}  ═══════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}  ✓ Quro Panel installed successfully${NC}"
    echo ""
    echo -e "  Panel URL:    ${protocol}://${DOMAIN}"
    echo -e "  API URL:      ${protocol}://${DOMAIN}/api"
    echo -e "  Admin email:  ${ADMIN_EMAIL}"
    echo -e "  Admin user:   ${ADMIN_USERNAME}"
    echo -e "  Install dir:  ${INSTALL_DIR}"
    echo ""
    echo -e "  ${YELLOW}Credentials saved to: ${INSTALL_DIR}/.credentials.txt${NC}"
    echo ""
    echo -e "  Useful commands:"
    echo -e "    cd ${INSTALL_DIR}"
    echo -e "    docker compose logs -f panel-api"
    echo -e "    docker compose logs -f panel-ui"
    echo -e "    docker compose down"
    echo -e "    docker compose up -d"
    echo ""
}

main() {
    print_banner
    require_root
    check_os
    collect_inputs
    install_packages
    install_docker
    clone_repo
    write_env
    configure_nginx
    configure_firewall
    start_services
    wait_for_api
    configure_ssl
    install_scripts
    save_credentials
    print_summary
}

main "$@"
