package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Claims espelha o que o Lambda authorizer injetaria no context (spec §7).
// O user id vem do claim padrão "sub" (jwt.RegisteredClaims.Subject) --
// mesmo claim usado pela authentication-api/authorizer reais e pelo
// users-api (pkgjwt.Claims.Subject), não um campo "userId" customizado.
type Claims struct {
	Role string `json:"role"` // administrator | user
	jwt.RegisteredClaims
}

// AuthMiddleware simula o Lambda authorizer: valida o JWT (HS256) e injeta
// userId/role no contexto da requisição. Nunca consulta banco.
func AuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHORIZED"})
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid || claims.Subject == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHORIZED"})
			return
		}
		c.Set("userId", claims.Subject)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// RequireRole restringe a rota a um role específico (ex.: administrator).
func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("role") != role {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN"})
			return
		}
		c.Next()
	}
}
