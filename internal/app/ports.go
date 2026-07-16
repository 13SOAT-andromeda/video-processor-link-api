package app

import (
	"context"
	"time"

	"github.com/fiap/links-service/internal/domain/link"
)

// LinkRepository é a porta de persistência (DynamoDB — tabelas Links e LinkEvents).
type LinkRepository interface {
	Save(ctx context.Context, l *link.Link) error
	Update(ctx context.Context, l *link.Link, expectedStatus link.Status) error
	Get(ctx context.Context, linkID string) (*link.Link, error)
	ListAll(ctx context.Context) ([]link.Link, error)
	ListByUser(ctx context.Context, userID string) ([]link.Link, error)
	SaveEvent(ctx context.Context, ev *link.Event) error
	ListEvents(ctx context.Context, linkID string) ([]link.Event, error)
}

// Storage é a porta do S3 (presigned URLs).
type Storage interface {
	PresignPut(ctx context.Context, key string, expires time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, expires time.Duration) (string, error)
}

// User é o retorno mínimo do users-service (GET /internal/users/:id).
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UsersClient resolve dados de usuário para notificação (ADR-008).
// A svc real não existe ainda — há implementação mock (adapters/users).
type UsersClient interface {
	GetUser(ctx context.Context, userID string) (*User, error)
}

// NotificationPayload segue o contrato confirmado do notification-service (ADR-008).
type NotificationPayload struct {
	Recipient struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"recipient"`
	Data struct {
		FileName string `json:"fileName"`
		LinkID   string `json:"linkId"`
		Reason   string `json:"reason"`
	} `json:"data"`
}

// Notifier publica no SNS notification-topic com templateType em MessageAttributes.
type Notifier interface {
	NotifyProcessingFailed(ctx context.Context, p NotificationPayload) error
}
