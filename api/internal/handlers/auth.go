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

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Role     string `json:"role"`
}

type UserResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	Token     string    `json:"token"`
}

func Login(db *pgxpool.Pool, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var id, username, email, role, passwordHash string
		var createdAt time.Time
		err := db.QueryRow(c.Request.Context(), database.FindUserByUsernameOrEmail, req.Username).
			Scan(&id, &username, &email, &passwordHash, &role, &createdAt)
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
			"role":     role,
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
			Role:      role,
			CreatedAt: createdAt,
			Token:     tokenString,
		})
	}
}

func Register(db *pgxpool.Pool, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req RegisterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}

		var user UserResponse
		var createdAt time.Time
		err = db.QueryRow(c.Request.Context(), database.CreateUserWithRole, req.Username, req.Email, string(hash), "user").
			Scan(&user.ID, &user.Username, &user.Email, &createdAt)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "username or email already registered"})
			return
		}
		user.Role = "user"
		user.CreatedAt = createdAt

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":      user.ID,
			"username": user.Username,
			"email":    user.Email,
			"role":     "user",
			"iat":      time.Now().Unix(),
			"exp":      time.Now().Add(72 * time.Hour).Unix(),
		})

		tokenString, err := token.SignedString([]byte(jwtSecret))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
			return
		}

		user.Token = tokenString
		c.JSON(http.StatusCreated, user)
	}
}

func Me(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		username, _ := c.Get("user_username")
		email, _ := c.Get("user_email")
		role, _ := c.Get("user_role")

		c.JSON(http.StatusOK, gin.H{
			"id":       userID,
			"username": username,
			"email":    email,
			"role":     role,
		})
	}
}

func ListUsers(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("user_role")
		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin only"})
			return
		}

		rows, err := db.Query(c.Request.Context(), `SELECT id, username, email, role, created_at FROM users ORDER BY created_at DESC`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
			return
		}
		defer rows.Close()

		type UserItem struct {
			ID        string `json:"id"`
			Username  string `json:"username"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			CreatedAt time.Time `json:"created_at"`
		}

		var users []UserItem
		for rows.Next() {
			var u UserItem
			if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.CreatedAt); err != nil {
				continue
			}
			users = append(users, u)
		}
		if users == nil {
			users = []UserItem{}
		}
		c.JSON(http.StatusOK, users)
	}
}

func AdminCreateUser(db *pgxpool.Pool, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		currentRole, _ := c.Get("user_role")
		if currentRole != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin only"})
			return
		}

		var req RegisterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		userRole := req.Role
		if userRole != "admin" && userRole != "user" {
			userRole = "user"
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}

		var user UserResponse
		var createdAt time.Time
		err = db.QueryRow(c.Request.Context(), database.CreateUserWithRole, req.Username, req.Email, string(hash), userRole).
			Scan(&user.ID, &user.Username, &user.Email, &createdAt)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "username or email already exists"})
			return
		}
		user.Role = userRole
		user.CreatedAt = createdAt

		c.JSON(http.StatusCreated, gin.H{
			"id":         user.ID,
			"username":   user.Username,
			"email":      user.Email,
			"role":       user.Role,
			"created_at": user.CreatedAt,
		})
	}
}
