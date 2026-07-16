// Gerador de JWT para desenvolvimento — simula o que a Lambda authentication
// emitiria. Uso: go run ./cmd/token -user u-123 -role user
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/fiap/links-service/internal/adapters/httpapi"
	"github.com/fiap/links-service/internal/config"
)

func main() {
	userID := flag.String("user", "u-123", "userId do token")
	role := flag.String("role", "user", "role: user | administrator")
	flag.Parse()

	cfg := config.Load()
	claims := httpapi.Claims{
		UserID: *userID,
		Role:   *role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), // 1h, como na spec §7
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Println(token)
}
