package handlers

import (
	"time"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quro/panel-api/internal/database"
)

type CreateNodeRequest struct {
	Name    string `json:"name" binding:"required"`
	Address string `json:"address" binding:"required"`
	Port    int    `json:"port" binding:"required,min=1,max=65535"`
}

type HeartbeatRequest struct {
	NodeID    string                 `json:"node_id"`
	Metrics   map[string]interface{} `json:"metrics"`
	Version   string                 `json:"version"`
	Timestamp string                 `json:"timestamp"`
}

type NodeResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
	Token         string `json:"token,omitempty"`
	Status        string `json:"status"`
	TotalRAM      int64  `json:"total_ram"`
	UsedRAM       int64  `json:"used_ram"`
	TotalCPU      int64  `json:"total_cpu"`
	UsedCPU       int64  `json:"used_cpu"`
	TotalDisk     int64  `json:"total_disk"`
	UsedDisk      int64  `json:"used_disk"`
	DaemonVersion string `json:"daemon_version"`
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func ListNodes(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(c.Request.Context(), database.ListNodes)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list nodes"})
			return
		}
		defer rows.Close()

		var nodes []NodeResponse
		for rows.Next() {
			var n NodeResponse
			var lastHeartbeat *time.Time
			if err := rows.Scan(&n.ID, &n.Name, &n.Address, &n.Port, &n.Token, &n.Status,
				&n.TotalRAM, &n.UsedRAM, &n.TotalCPU, &n.UsedCPU, &n.TotalDisk,
				&n.UsedDisk, &n.DaemonVersion, &lastHeartbeat, &n.CreatedAt); err != nil {
				continue
			}
			n.LastHeartbeat = lastHeartbeat
			nodes = append(nodes, n)
		}

		if nodes == nil {
			nodes = []NodeResponse{}
		}

		c.JSON(http.StatusOK, nodes)
	}
}

func GetNode(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var n NodeResponse
		var lastHeartbeat *time.Time

		err := db.QueryRow(c.Request.Context(), database.GetNode, id).
			Scan(&n.ID, &n.Name, &n.Address, &n.Port, &n.Token, &n.Status,
				&n.TotalRAM, &n.UsedRAM, &n.TotalCPU, &n.UsedCPU, &n.TotalDisk,
				&n.UsedDisk, &n.DaemonVersion, &lastHeartbeat, &n.CreatedAt)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
			return
		}
		n.LastHeartbeat = lastHeartbeat

		c.JSON(http.StatusOK, n)
	}
}

func CreateNode(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateNodeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		token := generateToken()
		var n NodeResponse
		var lastHeartbeat *time.Time
		err := db.QueryRow(c.Request.Context(), database.CreateNode, req.Name, req.Address, req.Port, token).
			Scan(&n.ID, &n.Name, &n.Address, &n.Port, &n.Token, &n.Status,
				&n.TotalRAM, &n.UsedRAM, &n.TotalCPU, &n.UsedCPU, &n.TotalDisk,
				&n.UsedDisk, &n.DaemonVersion, &lastHeartbeat, &n.CreatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create node"})
			return
		}

		c.JSON(http.StatusCreated, n)
	}
}

func GetNodeConfig(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var n NodeResponse
		var lastHeartbeat *time.Time

		err := db.QueryRow(c.Request.Context(), database.GetNode, id).
			Scan(&n.ID, &n.Name, &n.Address, &n.Port, &n.Token, &n.Status,
				&n.TotalRAM, &n.UsedRAM, &n.TotalCPU, &n.UsedCPU, &n.TotalDisk,
				&n.UsedDisk, &n.DaemonVersion, &lastHeartbeat, &n.CreatedAt)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
			return
		}

		cfg := map[string]interface{}{
			"panel_url":   getPanelURL(c),
			"node_id":     n.ID,
			"token":       n.Token,
			"node_name":   n.Name,
			"port":        n.Port,
			"data_dir":    "/var/lib/quro",
			"docker_host": "unix:///var/run/docker.sock",
			"version":     n.DaemonVersion,
		}
		c.JSON(http.StatusOK, cfg)
	}
}

func getPanelURL(c *gin.Context) string {
	origin := c.Request.Header.Get("Origin")
	if origin != "" {
		return origin
	}
	return "http://localhost:8080"
}

func HandleHeartbeat(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var req HeartbeatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		metricsMap, ok := req.Metrics["metrics"].(map[string]interface{})
		if !ok {
			metricsMap = req.Metrics
		}

		usedRAM := extractInt64(metricsMap, "ram", "used")
		usedCPU := extractFloat64(metricsMap, "cpu", "percent")
		usedDisk := extractInt64(metricsMap, "disk", "used")
		totalRAM := extractInt64(metricsMap, "ram", "total")
		totalCPU := extractInt(metricsMap, "cpu", "total")
		totalDisk := extractInt64(metricsMap, "disk", "total")
		daemonVersion := req.Version
		if daemonVersion == "" {
			daemonVersion = extractString(metricsMap, "version")
		}

		_, err := db.Exec(c.Request.Context(), database.UpdateNodeHeartbeat,
			usedRAM, int64(usedCPU), usedDisk,
			totalRAM, int64(totalCPU), totalDisk,
			daemonVersion, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update heartbeat"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func extractInt64(m map[string]interface{}, keys ...string) int64 {
	for i := 0; i < len(keys)-1; i++ {
		if sub, ok := m[keys[i]].(map[string]interface{}); ok {
			return extractInt64(sub, keys[i+1:]...)
		}
	}
	if v, ok := m[keys[len(keys)-1]].(float64); ok {
		return int64(v)
	}
	return 0
}

func extractFloat64(m map[string]interface{}, keys ...string) float64 {
	for i := 0; i < len(keys)-1; i++ {
		if sub, ok := m[keys[i]].(map[string]interface{}); ok {
			return extractFloat64(sub, keys[i+1:]...)
		}
	}
	if v, ok := m[keys[len(keys)-1]].(float64); ok {
		return v
	}
	return 0
}

func extractInt(m map[string]interface{}, keys ...string) int {
	for i := 0; i < len(keys)-1; i++ {
		if sub, ok := m[keys[i]].(map[string]interface{}); ok {
			return extractInt(sub, keys[i+1:]...)
		}
	}
	if v, ok := m[keys[len(keys)-1]].(float64); ok {
		return int(v)
	}
	return 0
}

func extractString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func DeleteNode(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		_, err := db.Exec(c.Request.Context(), database.DeleteNode, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete node"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "node deleted"})
	}
}
