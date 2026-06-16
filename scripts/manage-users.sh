#!/usr/bin/env bash
set -euo pipefail

# Quro user management script
# Usage:
#   ./manage-users.sh add admin alice@example.com MyP@ssw0rd
#   ./manage-users.sh list
#   ./manage-users.sh remove alice@example.com
#   ./manage-users.sh password alice@example.com NewP@ssw0rd

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/../.env"
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck source=/dev/null
  source "$ENV_FILE"
  set +a
fi

DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-quro}"
DB_PASSWORD="${DB_PASSWORD:-quro_secret}"
DB_NAME="${DB_NAME:-quro}"

export PGPASSWORD="$DB_PASSWORD"

PSQL=(psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -A -q)

hash_password() {
  local password="$1"
  # Use openssl to generate a bcrypt-like hash? No, use Python's bcrypt if available.
  python3 -c "import bcrypt, sys; print(bcrypt.hashpw(sys.argv[1].encode(), bcrypt.gensalt()).decode())" "$password"
}

ensure_psql() {
  if ! command -v psql &>/dev/null; then
    echo "Error: psql is required. Install with: sudo apt install postgresql-client"
    exit 1
  fi
}

cmd_add() {
  local role="${1:-admin}"
  local email="${2:-}"
  local password="${3:-}"

  if [ -z "$email" ] || [ -z "$password" ]; then
    echo "Usage: $0 add <role> <email> <password>"
    exit 1
  fi

  if [ "$role" != "admin" ] && [ "$role" != "user" ]; then
    echo "Error: role must be 'admin' or 'user'"
    exit 1
  fi

  if ! [[ "$email" =~ @ ]]; then
    echo "Error: invalid email"
    exit 1
  fi

  if [ "${#password}" -lt 8 ]; then
    echo "Error: password must be at least 8 characters"
    exit 1
  fi

  local username="${email%%@*}"
  local hash
  hash="$(hash_password "$password")"

  local exists
  exists="$(${PSQL[@]} -c "SELECT id FROM users WHERE email='$(echo "$email" | sed "s/'/''/g")' OR username='$(echo "$username" | sed "s/'/''/g")' LIMIT 1" 2>/dev/null || true)"

  if [ -n "$exists" ]; then
    echo "User already exists: $email"
    exit 1
  fi

  ${PSQL[@]} -c "
    INSERT INTO users (username, email, password_hash, role)
    VALUES ('$(echo "$username" | sed "s/'/''/g")',
            '$(echo "$email" | sed "s/'/''/g")',
            '$(echo "$hash" | sed "s/'/''/g")',
            '$role');
  "
  echo "Added $role: $email (username: $username)"
}

cmd_list() {
  ${PSQL[@]} -c "
    SELECT id, username, email, role, created_at
    FROM users
    ORDER BY created_at DESC;
  " | while IFS='|' read -r id username email role created_at; do
    printf "%-36s %-16s %-30s %-8s %s\n" "$id" "$username" "$email" "$role" "$created_at"
  done
}

cmd_remove() {
  local email="${1:-}"
  if [ -z "$email" ]; then
    echo "Usage: $0 remove <email>"
    exit 1
  fi

  ${PSQL[@]} -c "
    DELETE FROM users WHERE email='$(echo "$email" | sed "s/'/''/g")';
  "
  echo "Removed user: $email"
}

cmd_password() {
  local email="${1:-}"
  local password="${2:-}"
  if [ -z "$email" ] || [ -z "$password" ]; then
    echo "Usage: $0 password <email> <new_password>"
    exit 1
  fi

  local hash
  hash="$(hash_password "$password")"

  ${PSQL[@]} -c "
    UPDATE users
    SET password_hash='$(echo "$hash" | sed "s/'/''/g")'
    WHERE email='$(echo "$email" | sed "s/'/''/g")';
  "
  echo "Password updated for: $email"
}

main() {
  local cmd="${1:-help}"
  shift || true

  case "$cmd" in
    add) ensure_psql; cmd_add "$@" ;;
    list) ensure_psql; cmd_list ;;
    remove) ensure_psql; cmd_remove "$@" ;;
    password) ensure_psql; cmd_password "$@" ;;
    help|--help|-h)
      cat <<EOF
Quro user management

Usage:
  $0 add <role> <email> <password>   Add a user (role: admin|user)
  $0 list                            List users
  $0 remove <email>                  Remove a user
  $0 password <email> <password>     Change password

Examples:
  $0 add admin admin@quro.local MySecurePass123
  $0 add user bob@example.com BobPass123
  $0 list
  $0 remove bob@example.com
  $0 password admin@quro.local NewPass123
EOF
      ;;
    *)
      echo "Unknown command: $cmd"
      echo "Run '$0 help' for usage."
      exit 1
      ;;
  esac
}

main "$@"
