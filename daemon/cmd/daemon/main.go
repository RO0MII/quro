package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quro/daemon/internal/config"
	"github.com/quro/daemon/internal/container"
	"github.com/quro/daemon/internal/daemon"
	"github.com/quro/daemon/internal/metrics"
	"github.com/quro/daemon/internal/server"
)

func main() {
	cfg := config.Load()

	// Try to load config file if present
	if fileCfg, err := daemon.LoadConfigFile("/etc/quro/wings.json"); err == nil {
		cfg = fileCfg
		log.Printf("loaded config from /etc/quro/wings.json")
	}

	log.Printf("Quro Daemon v%s starting...", cfg.Version)

	mgr, err := container.NewManager()
	if err != nil {
		log.Fatalf("failed to create container manager: %v", err)
	}

	collector := metrics.NewCollector()

	dmn := daemon.New(cfg, mgr, collector)
	if err := dmn.Register(); err != nil {
		log.Printf("warning: failed to register with panel: %v", err)
	}

	srv := server.New(cfg, mgr, collector, dmn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		log.Printf("daemon API listening on :%d", cfg.Port)
		if err := srv.Start(); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	go pollContainerStats(ctx, mgr, collector)
	go pollHeartbeat(ctx, dmn)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down daemon...")
	cancel()
	srv.Shutdown()
	mgr.Close()
}

func pollContainerStats(ctx context.Context, mgr *container.Manager, collector *metrics.Collector) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			containers, err := mgr.ListManagedContainers(ctx)
			if err != nil {
				log.Printf("failed to list containers: %v", err)
				continue
			}

			for _, c := range containers {
				serverID := c.Labels["quro.server_id"]
				if serverID == "" {
					continue
				}

				stats, err := mgr.GetContainerStats(ctx, c.ID)
				if err != nil {
					continue
				}

				cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
				systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
				cpuPercent := 0.0
				if systemDelta > 0 && cpuDelta > 0 {
					cpuPercent = (cpuDelta / systemDelta) * float64(len(stats.CPUStats.CPUUsage.PercpuUsage)) * 100
				}

				collector.UpdateContainerStats(serverID, metrics.ContainerMetrics{
					ServerID:   serverID,
					CPUPercent: cpuPercent,
					RAMUsage:   stats.MemoryStats.Usage - stats.MemoryStats.Stats.Cache,
					RAMLimit:   stats.MemoryStats.Limit,
					NetworkRx:  stats.Networks["eth0"].RxBytes,
					NetworkTx:  stats.Networks["eth0"].TxBytes,
				})
			}
		}
	}
}

func pollHeartbeat(ctx context.Context, dmn *daemon.Daemon) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := dmn.SendHeartbeat(); err != nil {
				log.Printf("heartbeat error: %v", err)
			}
		}
	}
}
