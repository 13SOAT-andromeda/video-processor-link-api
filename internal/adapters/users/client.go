// Package users implementa app.UsersClient.
//
// A svc de Users ainda não está no ar — MockClient responde qualquer userId
// com um usuário determinístico. HTTPClient implementa o contrato real do
// video-processor-users-api (GET /api/users/:id — dono do recurso OU
// administrator, ADR-012): como o consumer da status-queue não tem JWT de
// usuário, o client assina um service token próprio (HS256, mesmo segredo
// compartilhado jwt-signing-key da plataforma) com role administrator.
// Basta trocar USE_USER_SVC_MOCK=false quando a svc ficar pronta.
package users

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/fiap/links-service/internal/app"
)

// MockClient — implementação mock enquanto o users-service não existe.
type MockClient struct{}

func NewMockClient() *MockClient { return &MockClient{} }

func (m *MockClient) GetUser(_ context.Context, userID string) (*app.User, error) {
	return &app.User{
		ID:    userID,
		Name:  "Mock User " + shortID(userID),
		Email: fmt.Sprintf("user-%s@mock.local", shortID(userID)),
	}, nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// HTTPClient — implementação real da chamada ao users-api dentro do cluster.
type HTTPClient struct {
	baseURL   string
	jwtSecret []byte
	http      *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

func NewHTTPClient(baseURL, jwtSecret string) *HTTPClient {
	return &HTTPClient{
		baseURL:   baseURL,
		jwtSecret: []byte(jwtSecret),
		http:      &http.Client{Timeout: 5 * time.Second},
	}
}

// serviceToken assina (e cacheia) um JWT de serviço com role administrator —
// o users-api valida o token por conta própria com o mesmo segredo
// compartilhado (jwt-signing-key), então não há chamada à authentication aqui.
func (c *HTTPClient) serviceToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.tokenExp) > time.Minute {
		return c.token, nil
	}
	exp := time.Now().Add(15 * time.Minute)
	claims := jwt.MapClaims{
		"userId": "links-service",
		"role":   "administrator",
		"iat":    time.Now().Unix(),
		"exp":    exp.Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(c.jwtSecret)
	if err != nil {
		return "", err
	}
	c.token, c.tokenExp = signed, exp
	return signed, nil
}

// userResponse aceita tanto o objeto direto quanto o envelope
// {"status":"success","data":{...}} do padrão base_response da plataforma —
// o users-api ainda não tem código, então o shape exato não está fixado.
type userResponse struct {
	app.User
	Data *app.User `json:"data"`
}

func (c *HTTPClient) GetUser(ctx context.Context, userID string) (*app.User, error) {
	// /api/users, não /users: essa chamada vai direto pro pod (Service
	// interno do cluster), sem passar pelo Gateway — então precisa da rota
	// real do users-api, não da rota pública sem prefixo que o Gateway expõe
	// (ver nota equivalente em httpapi.Router sobre a convenção /api).
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/users/"+userID, nil)
	if err != nil {
		return nil, err
	}
	token, err := c.serviceToken()
	if err != nil {
		return nil, fmt.Errorf("signing service token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("users-api returned %d for user %s", resp.StatusCode, userID)
	}
	var body userResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	u := body.User
	if body.Data != nil {
		u = *body.Data
	}
	if u.ID == "" {
		u.ID = userID
	}
	return &u, nil
}
