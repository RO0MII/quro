package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/memory"
	"github.com/shirou/gopsutil/v3/host"
)

type SystemMetrics struct {
	CPU    CPUMetrics    `json:"cpu"`
	RAM    RAMMetrics    `json:"ram"`
	Disk   DiskMetrics   `json:"disk"`
	Uptime uint64        `json:"uptime"`
}

type CPUMetrics struct {
	Used    float64 `json:"used"`
	Total   int     `json:"total"`
	Percent float64 `json:"percent"`
}

type RAMMetrics struct {
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

type DiskMetrics struct {
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

type ContainerMetrics struct {
	ServerID    string  `json:"server_id"`
	CPUPercent  float64 `json:"cpu_percent"`
	RAMUsage    uint64  `json:"ram_usage"`
	RAMLimit    uint64  `json:"ram_limit"`
	NetworkRx   uint64  `json:"network_rx"`
	NetworkTx   uint64  `json:"network_tx"`
}

type Collector struct {
	mu              sync.RWMutex
	lastCPUTimes    []cpu.TimesStat
	containerStats  map[string]ContainerMetrics
}

func NewCollector() *Collector {
	times, _ := cpu.Times(false)
	return &Collector{
		lastCPUTimes:   times,
		containerStats: make(map[string]ContainerMetrics),
	}
}

func (c *Collector) CollectSystem(ctx context.Context) (*SystemMetrics, error) {
	cpuPercent, err := cpu.PercentWithContext(ctx, time.Second, false)
	if err != nil {
		return nil, err
	}

	mem, err := memory.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, err
	}

	diskUsage, err := disk.UsageWithContext(ctx, "/")
	if err != nil {
		return nil, err
	}

	hostInfo, err := host.InfoWithContext(ctx)
	if err != nil {
		return nil, err
	}

	cpuCount, _ := cpu.Counts(true)

	return &SystemMetrics{
		CPU: CPUMetrics{
			Used:    cpuPercent[0],
			Total:   cpuCount,
			Percent: cpuPercent[0],
		},
		RAM: RAMMetrics{
			Used:    mem.Used,
			Total:   mem.Total,
			Percent: mem.UsedPercent,
		},
		Disk: DiskMetrics{
			Used:    diskUsage.Used,
			Total:   diskUsage.Total,
			Percent: diskUsage.UsedPercent,
		},
		Uptime: hostInfo.Uptime,
	}, nil
}

func (c *Collector) UpdateContainerStats(serverID string, stats ContainerMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.containerStats[serverID] = stats
}

func (c *Collector) GetContainerStats(serverID string) (ContainerMetrics, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stats, ok := c.containerStats[serverID]
	return stats, ok
}

func (c *Collector) GetAllContainerStats() map[string]ContainerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]ContainerMetrics, len(c.containerStats))
	for k, v := range c.containerStats {
		result[k] = v
	}
	return result
}
