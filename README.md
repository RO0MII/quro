# Quro Panel

A Pterodactyl-style game server management panel built with **Next.js**, **Go (Gin)**, **PostgreSQL**, and **Docker**.

## Features

- **Dashboard** — Real-time CPU/RAM metrics, server overview
- **Server Management** — Create, start, stop, restart, console, file manager, backups, schedules
- **Node System** — Add remote nodes (Wings-style daemon), auto heartbeat, real-time resource tracking
- **Admin Panel** — Admin bar with user menu, settings page, user/node management CLI scripts
- **Daemon (Wings)** — Go daemon that manages Docker containers on remote nodes, sends heartbeats to panel
- **Docker Compose** — One-command production deploy with Nginx, SSL, PostgreSQL, Redis

## Quick Install (Panel)

```bash
curl -fsSL https://your-panel.com/install.sh | sudo bash
```

## Quick Install (Node/Daemon)

```bash
# From panel UI: Nodes → Add Node → copy install command
curl -fsSL https://your-panel.com/install-daemon.sh | sudo bash -s -- \
  https://your-panel.com <NODE_TOKEN> <NODE_ID>
```

## CLI Tools

```bash
# Node management
./scripts/manage-nodes.sh add node-1 203.0.113.10 8081
./scripts/manage-nodes.sh list
./scripts/manage-nodes.sh install node-1 https://panel.example.com

# User management
./scripts/manage-users.sh add admin admin@example.com MyPass123
./scripts/manage-users.sh add user bob@example.com BobPass123
./scripts/manage-users.sh list
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Frontend | Next.js 14, React, Tailwind CSS, Framer Motion |
| API | Go, Gin, pgx (PostgreSQL), JWT auth |
| Database | PostgreSQL 16, Redis 7 |
| Daemon | Go, Docker SDK, gopsutil |
| Deploy | Docker Compose, Nginx, Let's Encrypt |

## Project Structure

```
quro/
├── api/              Go API server
│   ├── cmd/server/   Entry point
│   └── internal/     Handlers, middleware, database
├── daemon/           Wings-style node daemon
│   ├── cmd/daemon/   Entry point
│   └── internal/     Config, container, metrics, server
├── panel/            Next.js frontend
│   └── src/          Pages, components, lib
├── scripts/          CLI management tools
├── install.sh        Panel installer
├── install-daemon.sh Node/daemon installer
└── docker-compose.yml
```

## License

MIT
