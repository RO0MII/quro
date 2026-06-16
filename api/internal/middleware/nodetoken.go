package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NodeTokenAuth validates requests using EITHER:
//   - X-Node-Token header (for daemons), OR
//   - JWT Bearer token (for panel UI)
//
// This allows both the panel and daemons to access the same endpoints.
func NodeTokenAuth(db *pgxpool.Pool, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		nodeID := c.Param("id")
		if nodeID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing node id"})
			return
		}

		// Try X-Node-Token first (daemon auth)
		if token := c.GetHeader("X-Node-Token"); token != "" {
			var storedToken string
			err := db.QueryRow(c.Request.Context(), `SELECT token FROM nodes WHERE id = $1`, nodeID).Scan(&storedToken)
			if err != nil || storedToken != token {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid node token"})
				return
			}
			c.Set("node_id", nodeID)
			c.Next()
			return
		}

		// Try JWT Bearer token (panel UI auth)
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
					if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, jwt.ErrSignatureInvalid
					}
					return []byte(jwtSecret), nil
				})
				if err == nil && token.Valid {
					c.Set("node_id", nodeID)
					c.Next()
					return
				}
			}
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: provide X-Node-Token or JWT"})
	}
}
