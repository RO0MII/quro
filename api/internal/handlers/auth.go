package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/quro/panel-api/internal/database"
)

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type UserResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
	Token     string `json:"token"`
}

func Login(db *pgxpool.Pool, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var id, username, email, passwordHash, createdAt string
		err := db.QueryRow(c.Request.Context(), database.FindUserByUsernameOrEmail, req.Username).
			Scan(&id, &username, &email, &passwordHash, &createdAt)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":      id,
			"username": username,
			"email":    email,
			"iat":      time.Now().Unix(),
			"exp":      time.Now().Add(72 * time.Hour).Unix(),
		})

		tokenString, err := token.SignedString([]byte(jwtSecret))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
			return
		}

		c.JSON(http.StatusOK, UserResponse{
			ID:        id,
			Username:  username,
			Email:     email,
			CreatedAt: createdAt,
			Token:     tokenString,
		})
	}
}

func AutoLogin(db *pgxpool.Pool, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var id, username, email, createdAt string
		err := db.QueryRow(c.Request.Context(), database.FindUserByUsername, "admin").
			Scan(&id, &username, &email, &createdAt)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "admin user not found"})
			return
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":      id,
			"username": username,
			"email":    email,
			"iat":      time.Now().Unix(),
			"exp":      time.Now().Add(72 * time.Hour).Unix(),
		})

		tokenString, err := token.SignedString([]byte(jwtSecret))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
			return
		}

		c.JSON(http.StatusOK, UserResponse{
			ID:        id,
			Username:  username,
			Email:     email,
			CreatedAt: createdAt,
			Token:     tokenString,
		})
	}
}
