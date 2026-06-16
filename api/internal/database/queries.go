package database

const (
	CreateServer = `
		INSERT INTO servers (name, node_id, server_type, minecraft_version, ram, cpu, disk, port, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'installing')
		RETURNING id, name, node_id, server_type, minecraft_version, status, ram, cpu, disk, port, container_id, startup_command, variables, notes, created_at
	`

	GetServer = `
		SELECT s.id, s.name, s.node_id, n.name as node_name, s.minecraft_version,
		       s.status, s.ram, s.cpu, s.disk, s.port, s.container_id, s.created_at
		FROM servers s
		LEFT JOIN nodes n ON n.id = s.node_id
		WHERE s.id = $1
	`

	ListServers = `
		SELECT s.id, s.name, s.node_id, n.name as node_name, s.minecraft_version,
		       s.status, s.ram, s.cpu, s.disk, s.port, s.container_id, s.created_at
		FROM servers s
		LEFT JOIN nodes n ON n.id = s.node_id
		ORDER BY s.created_at DESC
	`

	DeleteServer = `DELETE FROM servers WHERE id = $1`

	UpdateServerStatus = `UPDATE servers SET status = $1 WHERE id = $2`

	UpdateServerContainer = `UPDATE servers SET container_id = $1, status = 'running' WHERE id = $2`

	CreateNode = `
		INSERT INTO nodes (name, address, port, token) VALUES ($1, $2, $3, $4)
		RETURNING id, name, address, port, token, status, total_ram, used_ram, total_cpu, used_cpu, total_disk, used_disk, daemon_version, last_heartbeat, created_at
	`

	UpdateNodeHeartbeat = `
		UPDATE nodes SET
			used_ram = $1, used_cpu = $2, used_disk = $3,
			total_ram = GREATEST(total_ram, $4),
			total_cpu = GREATEST(total_cpu, $5),
			total_disk = GREATEST(total_disk, $6),
			status = 'connected',
			daemon_version = $7,
			last_heartbeat = NOW()
		WHERE id = $8
	`

	GetNode = `
		SELECT id, name, address, port, token, status, total_ram, used_ram,
		       total_cpu, used_cpu, total_disk, used_disk, daemon_version, last_heartbeat, created_at
		FROM nodes WHERE id = $1
	`

	GetNodeByToken = `
		SELECT id, name, address, port, token, status, total_ram, used_ram,
		       total_cpu, used_cpu, total_disk, used_disk, daemon_version, last_heartbeat, created_at
		FROM nodes WHERE token = $1
	`

	ListNodes = `
		SELECT id, name, address, port, token, status, total_ram, used_ram,
		       total_cpu, used_cpu, total_disk, used_disk, daemon_version, last_heartbeat, created_at
		FROM nodes ORDER BY created_at DESC
	`

	DeleteNode = `DELETE FROM nodes WHERE id = $1`

	FindUserByEmail = `SELECT id, username, email, password_hash, role, created_at FROM users WHERE email = $1`

	FindUserByUsername = `SELECT id, username, email, password_hash, role, created_at FROM users WHERE username = $1`

	FindUserByUsernameOrEmail = `
		SELECT id, username, email, password_hash, role, created_at FROM users
		WHERE username = $1 OR email = $1
	`

	CreateUser = `
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, $3)
		RETURNING id, username, email, created_at
	`

	CreateUserWithRole = `
		INSERT INTO users (username, email, password_hash, role) VALUES ($1, $2, $3, $4)
		RETURNING id, username, email, created_at
	`

	UpsertAdminUser = `
		INSERT INTO users (username, email, password_hash, role)
		VALUES ($1, $2, $3, 'admin')
		ON CONFLICT (username) DO UPDATE SET
			email = EXCLUDED.email,
			password_hash = EXCLUDED.password_hash,
			role = 'admin'
		RETURNING id, username, email, created_at
	`

	UpdateServerStartup = `
		UPDATE servers SET startup_command = $2, variables = $3 WHERE id = $1
		RETURNING id, name, node_id, minecraft_version, status, ram, cpu, disk, port, container_id, startup_command, variables, notes, created_at
	`

	CreateBackup = `
		INSERT INTO backups (server_id, name, path, size, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, server_id, name, path, size, status, created_at
	`

	ListBackups = `
		SELECT id, server_id, name, path, size, status, created_at
		FROM backups WHERE server_id = $1 ORDER BY created_at DESC
	`

	GetBackup = `
		SELECT id, server_id, name, path, size, status, created_at
		FROM backups WHERE id = $1
	`

	DeleteBackup = `DELETE FROM backups WHERE id = $1`

	CreateSchedule = `
		INSERT INTO schedules (server_id, name, cron, action, payload, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, server_id, name, cron, action, payload, enabled, created_at
	`

	ListSchedules = `
		SELECT id, server_id, name, cron, action, payload, enabled, created_at
		FROM schedules WHERE server_id = $1 ORDER BY created_at DESC
	`

	GetSchedule = `
		SELECT id, server_id, name, cron, action, payload, enabled, created_at
		FROM schedules WHERE id = $1
	`

	UpdateSchedule = `
		UPDATE schedules SET name = $2, cron = $3, action = $4, payload = $5, enabled = $6
		WHERE id = $1
		RETURNING id, server_id, name, cron, action, payload, enabled, created_at
	`

	DeleteSchedule = `DELETE FROM schedules WHERE id = $1`
)
