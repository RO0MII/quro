package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"

	"github.com/quro/panel-api/internal/websocket"
)

var upgrader = gorilla.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func HandleConsoleWS(c *gin.Context, hub *websocket.Hub, serverID string) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("console WS upgrade error: %v", err)
		return
	}

	client := &websocket.Client{
		Hub:    hub,
		Conn:   conn,
		Send:   make(chan []byte, 256),
		RoomID: "console:" + serverID,
	}
	hub.Register <- client

	systemMsg, _ := json.Marshal(websocket.Message{
		Type: "system",
		Data: map[string]string{
			"data":      "Connected to console",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
	client.Send <- systemMsg

	go client.WritePump()
	go client.ReadPump()
}

func HandleMetricsWS(c *gin.Context, hub *websocket.Hub, serverID string) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("metrics WS upgrade error: %v", err)
		return
	}

	client := &websocket.Client{
		Hub:    hub,
		Conn:   conn,
		Send:   make(chan []byte, 256),
		RoomID: "metrics:" + serverID,
	}
	hub.Register <- client

	go client.WritePump()
	go client.ReadPump()
}
