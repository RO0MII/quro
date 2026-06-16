package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var globalPool *pgxpool.Pool

func SetPool(pool *pgxpool.Pool) {
	globalPool = pool
}

func GetDB() *pgxpool.Pool {
	return globalPool
}

func Connect(databaseURL string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	config.MaxConns = 25
	config.MinConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	return pool, nil
}

func EnsureAdminUser(pool *pgxpool.Pool, username, email, password string) error {
	ctx := context.Background()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, UpsertAdminUser, username, email, string(hash))
	return err
}

func Migrate(pool *pgxpool.Pool) error {
	ctx := context.Background()

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		username VARCHAR(255) UNIQUE NOT NULL,
		email VARCHAR(255) UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role VARCHAR(20) NOT NULL DEFAULT 'admin',
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS nodes (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name VARCHAR(255) NOT NULL,
		address VARCHAR(255) NOT NULL,
		port INTEGER NOT NULL DEFAULT 8081,
		token VARCHAR(255) UNIQUE NOT NULL,
		daemon_version VARCHAR(50) DEFAULT '0.1.0',
		total_ram BIGINT DEFAULT 0,
		used_ram BIGINT DEFAULT 0,
		total_cpu BIGINT DEFAULT 0,
		used_cpu BIGINT DEFAULT 0,
		total_disk BIGINT DEFAULT 0,
		used_disk BIGINT DEFAULT 0,
		status VARCHAR(20) DEFAULT 'disconnected',
		last_heartbeat TIMESTAMPTZ,
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS servers (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name VARCHAR(255) NOT NULL,
		node_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
		server_type VARCHAR(50) NOT NULL DEFAULT 'vanilla',
		minecraft_version VARCHAR(20) NOT NULL DEFAULT '1.21.1',
		status VARCHAR(20) DEFAULT 'stopped',
		ram INTEGER NOT NULL DEFAULT 1024,
		cpu INTEGER NOT NULL DEFAULT 100,
		disk INTEGER NOT NULL DEFAULT 2048,
		port INTEGER NOT NULL,
		container_id VARCHAR(255),
		startup_command TEXT,
		variables JSONB DEFAULT '{}',
		notes TEXT,
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_servers_node_id ON servers(node_id);
	CREATE INDEX IF NOT EXISTS idx_servers_status ON servers(status);

	CREATE TABLE IF NOT EXISTS backups (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		server_id UUID REFERENCES servers(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		path TEXT NOT NULL,
		size BIGINT DEFAULT 0,
		status VARCHAR(20) DEFAULT 'pending',
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_backups_server_id ON backups(server_id);

	CREATE TABLE IF NOT EXISTS schedules (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		server_id UUID REFERENCES servers(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		cron VARCHAR(50) NOT NULL,
		action VARCHAR(50) NOT NULL,
		payload JSONB DEFAULT '{}',
		enabled BOOLEAN DEFAULT TRUE,
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_schedules_server_id ON schedules(server_id);

	ALTER TABLE users ADD COLUMN IF NOT EXISTS username VARCHAR(255);
	`

	_, err := pool.Exec(ctx, schema)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `UPDATE users SET username = email WHERE username IS NULL`)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `ALTER TABLE users ALTER COLUMN username SET NOT NULL`)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username)`)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'admin'`)
	return err
}
