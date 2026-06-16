package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/quro/daemon/internal/config"
	"github.com/quro/daemon/internal/container"
	"github.com/quro/daemon/internal/daemon"
	"github.com/quro/daemon/internal/metrics"
)

type Server struct {
	cfg       *config.Config
	mgr       *container.Manager
	collector *metrics.Collector
	daemon    *daemon.Daemon
	http      *http.Server
	upgrader  websocket.Upgrader
}

func New(cfg *config.Config, mgr *container.Manager, collector *metrics.Collector, d *daemon.Daemon) *Server {
	return &Server{
		cfg:       cfg,
		mgr:       mgr,
		collector: collector,
		daemon:    d,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/containers", s.handleContainers)
	mux.HandleFunc("/api/containers/", s.handleContainerByID)
	mux.HandleFunc("/api/containers/", s.handleContainerAction)
	mux.HandleFunc("/api/metrics", s.handleMetrics)
	mux.HandleFunc("/api/metrics/system", s.handleSystemMetrics)
	mux.HandleFunc("/ws/logs/", s.handleLogStream)

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	s.http = &http.Server{
		Addr:    addr,
		Handler: withCORS(mux),
	}

	return s.http.ListenAndServe()
}

func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.http.Shutdown(ctx)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"version": s.cfg.Version,
		"uptime":  time.Since(startTime).String(),
	})
}

type createContainerRequest struct {
	ServerID         string `json:"server_id"`
	Name             string `json:"name"`
	MinecraftVersion string `json:"minecraft_version"`
	ServerType       string `json:"server_type"`
	RAM              int    `json:"ram"`
	CPU              int    `json:"cpu"`
	Disk             int    `json:"disk"`
	Port             int    `json:"port"`
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		containers, err := s.mgr.ListManagedContainers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		json.NewEncoder(w).Encode(containers)

	case "POST":
		var req createContainerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		containerID, err := s.mgr.CreateContainer(r.Context(), container.ContainerConfig{
			ServerID:         req.ServerID,
			Name:             req.Name,
			MinecraftVersion: req.MinecraftVersion,
			ServerType:       req.ServerType,
			RAM:              req.RAM,
			CPU:              req.CPU,
			Disk:             req.Disk,
			Port:             req.Port,
			DataDir:          s.cfg.DataDir,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"container_id": containerID,
			"status":       "created",
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleContainerByID(w http.ResponseWriter, r *http.Request) {
	id := extractID(r.URL.Path, "/api/containers/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container ID required")
		return
	}

	switch r.Method {
	case "GET":
		exists, err := s.mgr.ContainerExists(r.Context(), id)
		if err != nil || !exists {
			writeError(w, http.StatusNotFound, "container not found")
			return
		}
		logs, _ := s.mgr.GetContainerLogs(r.Context(), id, "50")
		json.NewEncoder(w).Encode(map[string]string{
			"container_id": id,
			"status":       "exists",
			"logs":         logs,
		})

	case "DELETE":
		if err := s.mgr.RemoveContainer(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "removed"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	id := extractID(path, "/api/containers/")

	if id == "" || r.Method != "POST" {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	action := filepath.Base(path)
	ctx := r.Context()

	switch action {
	case "start":
		err := s.mgr.StartContainer(ctx, id)
	case "stop":
		err := s.mgr.StopContainer(ctx, id)
	case "restart":
		err := s.mgr.RestartContainer(ctx, id)
	default:
		writeError(w, http.StatusBadRequest, "unknown action: "+action)
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"action": action,
		"status": "ok",
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	stats := s.collector.GetAllContainerStats()
	json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleSystemMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.collector.CollectSystem(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	json.NewEncoder(w).Encode(metrics)
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	containerID := extractID(r.URL.Path, "/ws/logs/")
	if containerID == "" {
		writeError(w, http.StatusBadRequest, "container ID required")
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	defer conn.Close()

	reader, err := s.mgr.StreamLogs(r.Context(), containerID)
	if err != nil {
		log.Printf("log stream error: %v", err)
		return
	}
	defer reader.Close()

	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			break
		}
		if n > 0 {
			conn.WriteMessage(websocket.TextMessage, buf[:n])
		}
	}
}

func extractID(path, prefix string) string {
	if len(path) <= len(prefix) {
		return ""
	}
	id := path[len(prefix):]
	if idx := strings.Index(id, "/"); idx > 0 {
		id = id[:idx]
	}
	return id
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

var startTime = time.Now()
