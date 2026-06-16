package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL  string
	RedisAddr    string
	JWTSecret    string
	PanelURL     string
	AdminEmail   string
	AdminPassword string
	Domain       string
}

func Load() *Config {
	godotenv.Load()

	host := getEnv("DB_HOST", "localhost")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "quro")
	password := getEnv("DB_PASSWORD", "quro_secret")
	dbname := getEnv("DB_NAME", "quro")

	return &Config{
		DatabaseURL:   "postgres://" + user + ":" + password + "@" + host + ":" + port + "/" + dbname + "?sslmode=disable",
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		JWTSecret:     getEnv("JWT_SECRET", "change_me_in_production"),
		PanelURL:      getEnv("PANEL_URL", "http://localhost:8080"),
		AdminEmail:    getEnv("ADMIN_EMAIL", "admin@quro.local"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "admin"),
		Domain:        getEnv("DOMAIN", "localhost"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
