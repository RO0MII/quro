package handlers

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quro/panel-api/internal/database"
	"github.com/quro/panel-api/internal/websocket"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

type CreateServerRequest struct {
	Name             string            `json:"name" binding:"required"`
	NodeID           string            `json:"node_id" binding:"required"`
	ServerType       string            `json:"server_type" binding:"required"`
	MinecraftVersion string            `json:"minecraft_version" binding:"required"`
	RAM              int               `json:"ram" binding:"required,min=256"`
	CPU              int               `json:"cpu" binding:"required,min=10"`
	Disk             int               `json:"disk" binding:"required,min=256"`
	Port             int               `json:"port" binding:"required,min=1024,max=65535"`
	StartupCommand   string            `json:"startup_command"`
	Variables        map[string]string `json:"variables"`
}

type ServerResponse struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	NodeID           string            `json:"node_id"`
	NodeName         string            `json:"node_name"`
	ServerType       string            `json:"server_type"`
	MinecraftVersion string            `json:"minecraft_version"`
	Status           string            `json:"status"`
	RAM              int               `json:"ram"`
	CPU              int               `json:"cpu"`
	Disk             int               `json:"disk"`
	Port             int               `json:"port"`
	ContainerID      string            `json:"container_id,omitempty"`
	StartupCommand   string            `json:"startup_command,omitempty"`
	Variables        map[string]string `json:"variables,omitempty"`
	Notes            string            `json:"notes,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

func ListServers(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(c.Request.Context(), database.ListServers)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list servers"})
			return
		}
		defer rows.Close()

		var servers []ServerResponse
		for rows.Next() {
			var s ServerResponse
			var containerID *string
			if err := rows.Scan(&s.ID, &s.Name, &s.NodeID, &s.NodeName,
				&s.MinecraftVersion, &s.Status, &s.RAM, &s.CPU, &s.Disk,
				&s.Port, &containerID, &s.CreatedAt); err != nil {
				continue
			}
			if containerID != nil {
				s.ContainerID = *containerID
			}
			servers = append(servers, s)
		}

		if servers == nil {
			servers = []ServerResponse{}
		}

		c.JSON(http.StatusOK, servers)
	}
}

func GetServer(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var s ServerResponse
		var containerID *string

		err := db.QueryRow(c.Request.Context(), database.GetServer, id).
			Scan(&s.ID, &s.Name, &s.NodeID, &s.NodeName, &s.MinecraftVersion,
				&s.Status, &s.RAM, &s.CPU, &s.Disk, &s.Port, &containerID, &s.CreatedAt)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		if containerID != nil {
			s.ContainerID = *containerID
		}

		c.JSON(http.StatusOK, s)
	}
}

func CreateServer(db *pgxpool.Pool, hub *websocket.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateServerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var s ServerResponse
		var containerID *string
		var notes *string
		var startupCmd *string
		var variables map[string]string
		err := db.QueryRow(c.Request.Context(), database.CreateServer,
			req.Name, req.NodeID, req.ServerType, req.MinecraftVersion, req.RAM, req.CPU, req.Disk, req.Port).
			Scan(&s.ID, &s.Name, &s.NodeID, &s.ServerType, &s.MinecraftVersion, &s.Status,
				&s.RAM, &s.CPU, &s.Disk, &s.Port, &containerID, &startupCmd, &variables, &notes, &s.CreatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create server"})
			return
		}
		if containerID != nil {
			s.ContainerID = *containerID
		}
		if notes != nil {
			s.Notes = *notes
		}
		if startupCmd != nil {
			s.StartupCommand = *startupCmd
		}
		if variables != nil {
			s.Variables = variables
		}

		// Deploy to the node via the daemon API
		go deployToNode(db, s, req)

		hub.Broadcast(websocket.Message{
			Type: "server:created",
			Data: s,
		})

		c.JSON(http.StatusCreated, s)
	}
}

func deployToNode(db *pgxpool.Pool, s ServerResponse, req CreateServerRequest) {
	var nodeAddress string
	var nodePort int
	var nodeToken string
	err := db.QueryRow(context.Background(),
		`SELECT address, port, token FROM nodes WHERE id = $1`, req.NodeID,
	).Scan(&nodeAddress, &nodePort, &nodeToken)
	if err != nil {
		log.Printf("deploy: failed to find node %s: %v", req.NodeID, err)
		return
	}

	daemonURL := fmt.Sprintf("http://%s:%d/api/containers", nodeAddress, nodePort)

	payload := map[string]interface{}{
		"server_id":         s.ID,
		"name":              s.Name,
		"minecraft_version": req.MinecraftVersion,
		"server_type":       req.ServerType,
		"ram":               req.RAM,
		"cpu":               req.CPU,
		"disk":              req.Disk,
		"port":              req.Port,
	}
	body, _ := json.Marshal(payload)

	httpReq, err := http.NewRequest(http.MethodPost, daemonURL, strings.NewReader(string(body)))
	if err != nil {
		log.Printf("deploy: failed to create request: %v", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Node-Token", nodeToken)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		log.Printf("deploy: failed to reach daemon at %s: %v", daemonURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("deploy: daemon returned %d: %s", resp.StatusCode, string(respBody))
		return
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if cid, ok := result["container_id"].(string); ok {
		log.Printf("deploy: server %s deployed as container %s on node %s", s.ID, cid, req.NodeID)
	}
}

func DeleteServer(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		_, err := db.Exec(c.Request.Context(), database.DeleteServer, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete server"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "server deleted"})
	}
}

func StartServer(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		_, err := db.Exec(c.Request.Context(), database.UpdateServerStatus, "running", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start server"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "server starting"})
	}
}

func StopServer(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		_, err := db.Exec(c.Request.Context(), database.UpdateServerStatus, "stopped", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stop server"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "server stopping"})
	}
}

func RestartServer(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		_, err := db.Exec(c.Request.Context(), database.UpdateServerStatus, "running", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to restart server"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "server restarting"})
	}
}

type CommandRequest struct {
	Command string `json:"command"`
}

func SendCommand(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CommandRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "command sent"})
	}
}

func GetMetrics(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var nodeAddress string
		var nodePort int
		var containerID *string
		err := db.QueryRow(c.Request.Context(), `
			SELECT n.address, n.port, s.container_id
			FROM servers s
			JOIN nodes n ON n.id = s.node_id
			WHERE s.id = $1
		`, id).Scan(&nodeAddress, &nodePort, &containerID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}

		daemonURL := fmt.Sprintf("http://%s:%d/api/metrics", nodeAddress, nodePort)
		resp, err := httpClient.Get(daemonURL)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"cpu":  gin.H{"used": 0, "total": 100, "percent": 0},
				"ram":  gin.H{"used": 0, "total": 4096, "percent": 0},
				"disk": gin.H{"used": 0, "total": 20480, "percent": 0},
				"uptime": 0,
				"node_error": err.Error(),
			})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var containerStats map[string]interface{}
		if err := json.Unmarshal(body, &containerStats); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"cpu":  gin.H{"used": 0, "total": 100, "percent": 0},
				"ram":  gin.H{"used": 0, "total": 4096, "percent": 0},
				"disk": gin.H{"used": 0, "total": 20480, "percent": 0},
				"uptime": 0,
				"parse_error": err.Error(),
			})
			return
		}

		if id != "" && containerStats[id] != nil {
			c.JSON(http.StatusOK, gin.H{"container": containerStats[id]})
			return
		}

		c.JSON(http.StatusOK, containerStats)
	}
}

// --- File Operations ---

var serverDataDir = "/var/lib/quro/servers"

func getServerDir(serverID string) string {
	return filepath.Join(serverDataDir, serverID)
}

func ListFiles(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		path := c.Query("path")
		if path == "" {
			path = "/"
		}

		fullPath := filepath.Join(getServerDir(id), path)
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "directory not found"})
			return
		}

		type FileEntry struct {
			Name        string `json:"name"`
			Path        string `json:"path"`
			Type        string `json:"type"`
			Size        int64  `json:"size"`
			ModifiedAt  string `json:"modified_at"`
			Permissions string `json:"permissions"`
		}

		var files []FileEntry
		for _, entry := range entries {
			info, _ := entry.Info()
			fileType := "file"
			if entry.IsDir() {
				fileType = "directory"
			}

			size := int64(0)
			modTime := ""
			mode := ""
			if info != nil {
				size = info.Size()
				modTime = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
				mode = fmt.Sprintf("%o", info.Mode().Perm())
			}

			files = append(files, FileEntry{
				Name:        entry.Name(),
				Path:        filepath.Join(path, entry.Name()),
				Type:        fileType,
				Size:        size,
				ModifiedAt:  modTime,
				Permissions: mode,
			})
		}

		c.JSON(http.StatusOK, files)
	}
}

func ReadFile(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		path := c.Query("path")
		fullPath := filepath.Join(getServerDir(id), path)

		data, err := os.ReadFile(fullPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"content": string(data)})
	}
}

type WriteFileRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func WriteFile(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req WriteFileRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fullPath := filepath.Join(getServerDir(id), req.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create directory"})
			return
		}

		if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write file"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "file written"})
	}
}

type DeleteFileRequest struct {
	Path string `json:"path"`
}

func DeleteFile(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req DeleteFileRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fullPath := filepath.Join(getServerDir(id), req.Path)
		if err := os.RemoveAll(fullPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "deleted"})
	}
}

type RenameFileRequest struct {
	OldPath string `json:"old_path" binding:"required"`
	NewPath string `json:"new_path" binding:"required"`
}

func RenameFile(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req RenameFileRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		oldFull := filepath.Join(getServerDir(id), req.OldPath)
		newFull := filepath.Join(getServerDir(id), req.NewPath)
		if err := os.Rename(oldFull, newFull); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rename"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "renamed"})
	}
}

type CreateFolderRequest struct {
	Path string `json:"path" binding:"required"`
}

func CreateFolder(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req CreateFolderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fullPath := filepath.Join(getServerDir(id), req.Path)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create folder"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "folder created"})
	}
}

func UploadFile(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		path := c.PostForm("path")
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no file uploaded"})
			return
		}

		fullPath := filepath.Join(getServerDir(id), path, filepath.Base(file.Filename))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create directory"})
			return
		}

		if err := c.SaveUploadedFile(file, fullPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "uploaded", "path": path})
	}
}

func UpdateServerStartup(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			StartupCommand string            `json:"startup_command"`
			Variables      map[string]string `json:"variables"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var s ServerResponse
		var containerID *string
		var notes *string
		var startupCmd *string
		var variables map[string]string
		err := db.QueryRow(c.Request.Context(), database.UpdateServerStartup, id, req.StartupCommand, req.Variables).
			Scan(&s.ID, &s.Name, &s.NodeID, &s.MinecraftVersion, &s.Status,
				&s.RAM, &s.CPU, &s.Disk, &s.Port, &containerID, &startupCmd, &variables, &notes, &s.CreatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update startup"})
			return
		}
		if containerID != nil {
			s.ContainerID = *containerID
		}
		if notes != nil {
			s.Notes = *notes
		}
		if startupCmd != nil {
			s.StartupCommand = *startupCmd
		}
		if variables != nil {
			s.Variables = variables
		}
		c.JSON(http.StatusOK, s)
	}
}

type BackupResponse struct {
	ID        string `json:"id"`
	ServerID  string `json:"server_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Status    string `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func CreateBackup(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.Name == "" {
			req.Name = "backup-" + time.Now().Format("20060102-150405")
		}

		backupDir := filepath.Join(serverDataDir, "backups", id)
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create backup directory"})
			return
		}

		backupPath := filepath.Join(backupDir, req.Name+".zip")
		serverDir := getServerDir(id)
		if _, err := os.Stat(serverDir); os.IsNotExist(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "server directory not found"})
			return
		}

		zipFile, err := os.Create(backupPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create backup file"})
			return
		}
		defer zipFile.Close()

		zipWriter := zip.NewWriter(zipFile)
		defer zipWriter.Close()

		err = filepath.Walk(serverDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			relPath, err := filepath.Rel(serverDir, path)
			if err != nil {
				return err
			}
			if relPath == "." {
				return nil
			}
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(relPath)
			if info.IsDir() {
				header.Name += "/"
				_, err = zipWriter.CreateHeader(header)
				return err
			}
			writer, err := zipWriter.CreateHeader(header)
			if err != nil {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(writer, file)
			return err
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to archive backup"})
			return
		}

		zipWriter.Close()
		info, _ := os.Stat(backupPath)
		var size int64
		if info != nil {
			size = info.Size()
		}

		var b BackupResponse
		err = db.QueryRow(c.Request.Context(), database.CreateBackup, id, req.Name, backupPath, size, "completed").
			Scan(&b.ID, &b.ServerID, &b.Name, &b.Path, &b.Size, &b.Status, &b.CreatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save backup record"})
			return
		}
		c.JSON(http.StatusCreated, b)
	}
}

func ListBackups(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		rows, err := db.Query(c.Request.Context(), database.ListBackups, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list backups"})
			return
		}
		defer rows.Close()

		var backups []BackupResponse
		for rows.Next() {
			var b BackupResponse
			if err := rows.Scan(&b.ID, &b.ServerID, &b.Name, &b.Path, &b.Size, &b.Status, &b.CreatedAt); err != nil {
				continue
			}
			backups = append(backups, b)
		}
		if backups == nil {
			backups = []BackupResponse{}
		}
		c.JSON(http.StatusOK, backups)
	}
}

func RestoreBackup(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		backupID := c.Param("backupId")

		var b BackupResponse
		err := db.QueryRow(c.Request.Context(), database.GetBackup, backupID).
			Scan(&b.ID, &b.ServerID, &b.Name, &b.Path, &b.Size, &b.Status, &b.CreatedAt)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
			return
		}
		if b.ServerID != id {
			c.JSON(http.StatusForbidden, gin.H{"error": "backup does not belong to this server"})
			return
		}

		serverDir := getServerDir(id)
		if err := os.RemoveAll(serverDir); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear server directory"})
			return
		}
		if err := os.MkdirAll(serverDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to recreate server directory"})
			return
		}

		zipReader, err := zip.OpenReader(b.Path)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open backup"})
			return
		}
		defer zipReader.Close()

		for _, file := range zipReader.File {
			fullPath := filepath.Join(serverDir, file.Name)
			if !strings.HasPrefix(fullPath, serverDir) {
				continue
			}
			if file.FileInfo().IsDir() {
				os.MkdirAll(fullPath, file.Mode())
				continue
			}
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to restore directory"})
				return
			}
			outFile, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to restore file"})
				return
			}
			inFile, err := file.Open()
			if err != nil {
				outFile.Close()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read backup file"})
				return
			}
			_, err = io.Copy(outFile, inFile)
			inFile.Close()
			outFile.Close()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write restored file"})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "backup restored"})
	}
}

func DeleteBackup(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		backupID := c.Param("backupId")

		var b BackupResponse
		err := db.QueryRow(c.Request.Context(), database.GetBackup, backupID).
			Scan(&b.ID, &b.ServerID, &b.Name, &b.Path, &b.Size, &b.Status, &b.CreatedAt)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
			return
		}
		if b.ServerID != id {
			c.JSON(http.StatusForbidden, gin.H{"error": "backup does not belong to this server"})
			return
		}

		os.Remove(b.Path)
		_, err = db.Exec(c.Request.Context(), database.DeleteBackup, backupID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete backup"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "backup deleted"})
	}
}

type ScheduleResponse struct {
	ID        string                 `json:"id"`
	ServerID  string                 `json:"server_id"`
	Name      string                 `json:"name"`
	Cron      string                 `json:"cron"`
	Action    string                 `json:"action"`
	Payload   map[string]interface{} `json:"payload"`
	Enabled   bool                   `json:"enabled"`
	CreatedAt time.Time              `json:"created_at"`
}

func CreateSchedule(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			Name    string                 `json:"name" binding:"required"`
			Cron    string                 `json:"cron" binding:"required"`
			Action  string                 `json:"action" binding:"required"`
			Payload map[string]interface{} `json:"payload"`
			Enabled bool                   `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var s ScheduleResponse
		var payload map[string]interface{}
		err := db.QueryRow(c.Request.Context(), database.CreateSchedule, id, req.Name, req.Cron, req.Action, req.Payload, req.Enabled).
			Scan(&s.ID, &s.ServerID, &s.Name, &s.Cron, &s.Action, &payload, &s.Enabled, &s.CreatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create schedule"})
			return
		}
		if payload != nil {
			s.Payload = payload
		}
		c.JSON(http.StatusCreated, s)
	}
}

func ListSchedules(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		rows, err := db.Query(c.Request.Context(), database.ListSchedules, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list schedules"})
			return
		}
		defer rows.Close()

		var schedules []ScheduleResponse
		for rows.Next() {
			var s ScheduleResponse
			var payload map[string]interface{}
			if err := rows.Scan(&s.ID, &s.ServerID, &s.Name, &s.Cron, &s.Action, &payload, &s.Enabled, &s.CreatedAt); err != nil {
				continue
			}
			if payload != nil {
				s.Payload = payload
			}
			schedules = append(schedules, s)
		}
		if schedules == nil {
			schedules = []ScheduleResponse{}
		}
		c.JSON(http.StatusOK, schedules)
	}
}

func UpdateSchedule(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		scheduleID := c.Param("scheduleId")
		var req struct {
			Name    string                 `json:"name" binding:"required"`
			Cron    string                 `json:"cron" binding:"required"`
			Action  string                 `json:"action" binding:"required"`
			Payload map[string]interface{} `json:"payload"`
			Enabled bool                   `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var s ScheduleResponse
		var payload map[string]interface{}
		err := db.QueryRow(c.Request.Context(), database.UpdateSchedule, scheduleID, req.Name, req.Cron, req.Action, req.Payload, req.Enabled).
			Scan(&s.ID, &s.ServerID, &s.Name, &s.Cron, &s.Action, &payload, &s.Enabled, &s.CreatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update schedule"})
			return
		}
		if s.ServerID != id {
			c.JSON(http.StatusForbidden, gin.H{"error": "schedule does not belong to this server"})
			return
		}
		if payload != nil {
			s.Payload = payload
		}
		c.JSON(http.StatusOK, s)
	}
}

func DeleteSchedule(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		scheduleID := c.Param("scheduleId")

		var s ScheduleResponse
		err := db.QueryRow(c.Request.Context(), database.GetSchedule, scheduleID).
			Scan(&s.ID, &s.ServerID, &s.Name, &s.Cron, &s.Action, &s.Payload, &s.Enabled, &s.CreatedAt)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
			return
		}
		if s.ServerID != id {
			c.JSON(http.StatusForbidden, gin.H{"error": "schedule does not belong to this server"})
			return
		}

		_, err = db.Exec(c.Request.Context(), database.DeleteSchedule, scheduleID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete schedule"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "schedule deleted"})
	}
}

func init() {
	if d := os.Getenv("SERVER_DATA_DIR"); d != "" {
		serverDataDir = d
	}
}
