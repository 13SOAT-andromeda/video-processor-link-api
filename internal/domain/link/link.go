package link

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("LINK_NOT_FOUND")
	ErrForbidden = errors.New("FORBIDDEN")
)

// RetentionDays é a retenção de 3 dias do zip (spec §1).
const RetentionDays = 3

// MaxFileSizeBytes limita o vídeo a 200 MB (spec §1).
const MaxFileSizeBytes = 200 * 1024 * 1024

// Link é o agregado raiz do domínio (tabela Links no DynamoDB).
type Link struct {
	LinkID         string  `json:"linkId" dynamodbav:"linkId"`
	ShareableURL   string  `json:"shareableUrl" dynamodbav:"shareableUrl"`
	FileName       string  `json:"fileName" dynamodbav:"fileName"`
	S3RawKey       string  `json:"s3RawKey" dynamodbav:"s3RawKey"`
	S3ProcessedKey *string `json:"s3ProcessedKey,omitempty" dynamodbav:"s3ProcessedKey,omitempty"`
	Status         Status  `json:"status" dynamodbav:"status"`
	UserID         string  `json:"userId" dynamodbav:"userId"`
	IsPrivate      bool    `json:"isPrivate" dynamodbav:"isPrivate"`
	ExpiresAt      int64   `json:"expiresAt" dynamodbav:"expiresAt"` // unix ts — TTL nativo do DynamoDB
	CreatedAt      string  `json:"createdAt" dynamodbav:"createdAt"` // ISO8601
	UpdatedAt      string  `json:"updatedAt" dynamodbav:"updatedAt"` // ISO8601
}

// Event registra uma transição de status (tabela LinkEvents).
type Event struct {
	LinkID     string         `json:"linkId" dynamodbav:"linkId"`
	CreatedAt  string         `json:"createdAt" dynamodbav:"createdAt"` // SK, ISO8601
	StatusFrom Status         `json:"statusFrom" dynamodbav:"statusFrom"`
	StatusTo   Status         `json:"statusTo" dynamodbav:"statusTo"`
	EventType  string         `json:"eventType" dynamodbav:"eventType"`
	Metadata   map[string]any `json:"metadata,omitempty" dynamodbav:"metadata,omitempty"`
	ExpiresAt  int64          `json:"-" dynamodbav:"expiresAt"` // TTL
}

// New cria um Link em LINK_CREATED.
func New(userID, fileName string, isPrivate bool, now time.Time) *Link {
	id := uuid.NewString()
	return &Link{
		LinkID:    id,
		FileName:  fileName,
		S3RawKey:  id + "/raw/" + fileName,
		Status:    StatusLinkCreated,
		UserID:    userID,
		IsPrivate: isPrivate,
		ExpiresAt: now.AddDate(0, 0, RetentionDays).Unix(),
		CreatedAt: now.UTC().Format(time.RFC3339),
		UpdatedAt: now.UTC().Format(time.RFC3339),
	}
}

// Transition aplica a transição e devolve o Event correspondente.
func (l *Link) Transition(to Status, eventType string, metadata map[string]any, now time.Time) (*Event, error) {
	if !CanTransition(l.Status, to) {
		return nil, ErrInvalidTransition{From: l.Status, To: to}
	}
	ev := &Event{
		LinkID:     l.LinkID,
		CreatedAt:  now.UTC().Format(time.RFC3339Nano),
		StatusFrom: l.Status,
		StatusTo:   to,
		EventType:  eventType,
		Metadata:   metadata,
		ExpiresAt:  now.AddDate(0, 0, RetentionDays).Unix(),
	}
	l.Status = to
	l.UpdatedAt = now.UTC().Format(time.RFC3339)
	return ev, nil
}

// OwnedBy confere posse do link (authorization de recurso, spec §7).
func (l *Link) OwnedBy(userID string) bool { return l.UserID == userID }
