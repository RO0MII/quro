package container

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	nat "github.com/docker/go-connections/nat"
)

type Manager struct {
	client *client.Client
}

type ContainerConfig struct {
	ServerID         string
	Name             string
	MinecraftVersion string
	ServerType       string
	RAM              int
	CPU              int
	Disk             int
	Port             int
	DataDir          string
}

func NewManager() (*Manager, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Manager{client: cli}, nil
}

func (m *Manager) CreateContainer(ctx context.Context, cfg ContainerConfig) (string, error) {
	dataDir := filepath.Join(cfg.DataDir, cfg.ServerID)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}

	serverType := "VANILLA"
	imageTag := "latest"
	switch cfg.ServerType {
	case "paper":
		serverType = "PAPER"
	case "forge":
		serverType = "FORGE"
	case "fabric":
		serverType = "FABRIC"
	case "spigot":
		serverType = "SPIGOT"
	default:
		serverType = "VANILLA"
	}

	env := []string{
		"EULA=TRUE",
		fmt.Sprintf("MEMORY=%dM", cfg.RAM),
		fmt.Sprintf("TYPE=%s", serverType),
		fmt.Sprintf("VERSION=%s", cfg.MinecraftVersion),
		fmt.Sprintf("MAX_PLAYERS=20"),
		fmt.Sprintf("DIFFICULTY=normal"),
		fmt.Sprintf("ONLINE_MODE=TRUE"),
		fmt.Sprintf("SPAWN_PROTECTION=16"),
		fmt.Sprintf("VIEW_DISTANCE=10"),
		fmt.Sprintf("SIMULATION_DISTANCE=10"),
		fmt.Sprintf("SERVER_PORT=25565"),
		fmt.Sprintf("STOP_SERVER_ON_DISABLE=TRUE"),
	}

	resp, err := m.client.ContainerCreate(ctx, &container.Config{
		Image: "itzg/minecraft-server:" + imageTag,
		Env:   env,
		Labels: map[string]string{
			"quro.managed":     "true",
			"quro.server_id":   cfg.ServerID,
			"quro.server_type": cfg.ServerType,
			"quro.version":     cfg.MinecraftVersion,
			"quro.name":        cfg.Name,
		},
	}, &container.HostConfig{
		Resources: container.Resources{
			Memory:     int64(cfg.RAM) * 1024 * 1024,
			CPUPercent: int64(cfg.CPU),
		},
		PortBindings: nat.PortMap{
			"25565/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", cfg.Port)},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
		Binds: []string{
			fmt.Sprintf("%s:/data", dataDir),
		},
	}, nil, nil, fmt.Sprintf("quro-%s", cfg.ServerID))
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := m.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return resp.ID, nil
}

func (m *Manager) StartContainer(ctx context.Context, containerID string) error {
	return m.client.ContainerStart(ctx, containerID, container.StartOptions{})
}

func (m *Manager) StopContainer(ctx context.Context, containerID string) error {
	timeout := 30
	return m.client.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	})
}

func (m *Manager) RestartContainer(ctx context.Context, containerID string) error {
	timeout := 30
	return m.client.ContainerRestart(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	})
}

func (m *Manager) RemoveContainer(ctx context.Context, containerID string) error {
	return m.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	})
}

func (m *Manager) GetContainerLogs(ctx context.Context, containerID string, tail string) (string, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
	}

	if tail != "" {
		options.Tail = tail
	} else {
		options.Follow = false
		options.Tail = "100"
	}

	reader, err := m.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var buf bytes.Buffer
	stdcopy.StdCopy(&buf, &buf, reader)
	return buf.String(), nil
}

func (m *Manager) StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	return m.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
}

func (m *Manager) ExecCommand(ctx context.Context, containerID string, cmd []string) (string, error) {
	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	resp, err := m.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", err
	}

	attachResp, err := m.client.ContainerExecAttach(ctx, resp.ID, types.ExecStartCheck{})
	if err != nil {
		return "", err
	}
	defer attachResp.Close()

	var buf bytes.Buffer
	stdcopy.StdCopy(&buf, &buf, attachResp.Reader)
	return buf.String(), nil
}

func (m *Manager) GetContainerStats(ctx context.Context, containerID string) (*types.StatsJSON, error) {
	stats, err := m.client.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer stats.Body.Close()

	var statsJSON types.StatsJSON
	if err := json.NewDecoder(stats.Body).Decode(&statsJSON); err != nil {
		return nil, err
	}

	return &statsJSON, nil
}

func (m *Manager) ListManagedContainers(ctx context.Context) ([]types.Container, error) {
	return m.client.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "quro.managed=true"),
		),
	})
}

func (m *Manager) ContainerExists(ctx context.Context, containerID string) (bool, error) {
	_, err := m.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (m *Manager) Close() error {
	return m.client.Close()
}
