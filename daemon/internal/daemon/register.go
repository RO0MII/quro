package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/quro/daemon/internal/config"
	"github.com/quro/daemon/internal/container"
	"github.com/quro/daemon/internal/metrics"
)

type Daemon struct {
	cfg       *config.Config
	mgr       *container.Manager
	collector *metrics.Collector
	client    *http.Client
	nodeID    string
}

func New(cfg *config.Config, mgr *container.Manager, collector *metrics.Collector) *Daemon {
	return &Daemon{
		cfg:       cfg,
		mgr:       mgr,
		collector: collector,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type registerRequest struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type registerResponse struct {
	ID string `json:"id"`
}

func (d *Daemon) Register() error {
	if d.cfg.NodeID == "" || d.cfg.Token == "" {
		return fmt.Errorf("node_id and token are required; create a node in the panel and run the install command")
	}

	d.nodeID = d.cfg.NodeID
	if err := d.verifyToken(); err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}

	log.Printf("daemon registered as node %s", d.nodeID)
	return nil
}

func (d *Daemon) verifyToken() error {
	req, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf("%s/api/nodes/%s/config", d.cfg.PanelURL, d.cfg.NodeID),
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("X-Node-Token", d.cfg.Token)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token verification returned status %d", resp.StatusCode)
	}

	return nil
}

func (d *Daemon) SendHeartbeat() error {
	sysMetrics, err := d.collector.CollectSystem(context.Background())
	if err != nil {
		return err
	}

	heartbeat := map[string]interface{}{
		"node_id": d.nodeID,
		"metrics": sysMetrics,
		"version": d.cfg.Version,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("%s/api/nodes/%s/heartbeat", d.cfg.PanelURL, d.nodeID),
		bytes.NewReader(data),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Node-Token", d.cfg.Token)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	return nil
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()

	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}

func LoadConfigFile(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
