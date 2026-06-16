package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Version     string `json:"version"`
	Port        int    `json:"port"`
	PanelURL    string `json:"panel_url"`
	Token       string `json:"token"`
	NodeID      string `json:"node_id"`
	NodeName    string `json:"node_name"`
	DataDir     string `json:"data_dir"`
	DockerHost  string `json:"docker_host"`
}

func Load() *Config {
	godotenv.Load()

	return &Config{
		Version:    getEnv("DAEMON_VERSION", "0.1.0"),
		Port:       getEnvInt("DAEMON_PORT", 8081),
		PanelURL:   getEnv("PANEL_URL", "http://localhost:8080"),
		Token:      getEnv("DAEMON_TOKEN", ""),
		NodeName:   getEnv("NODE_NAME", "default"),
		DataDir:    getEnv("DATA_DIR", "/var/lib/quro"),
		DockerHost: getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		var i int
		if _, err := fmt.Sscanf(value, "%d", &i); err == nil {
			return i
		}
	}
	return fallback
}
