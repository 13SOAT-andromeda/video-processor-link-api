// Package users implementa app.UsersClient.
//
// A svc de Users ainda não existe — MockClient responde qualquer userId com
// um usuário determinístico. HTTPClient já implementa o contrato real
// (GET /internal/users/:id, ClusterIP, sem JWT — spec §2 item 9) para quando
// a svc ficar pronta: basta trocar USERS_MODE=http.
package users

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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

// HTTPClient — implementação real da chamada interna ao users-service.
type HTTPClient struct {
	baseURL string
	http    *http.Client
}

func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{baseURL: baseURL, http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *HTTPClient) GetUser(ctx context.Context, userID string) (*app.User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/users/"+userID, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("users-service returned %d for user %s", resp.StatusCode, userID)
	}
	var u app.User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}
