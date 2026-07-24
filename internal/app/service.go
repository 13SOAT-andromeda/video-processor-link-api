package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fiap/links-service/internal/domain/link"
)

const presignExpiry = time.Hour // spec §7: presigned com expiração curta (1h)

// Service implementa os casos de uso do links-service.
type Service struct {
	repo     LinkRepository
	storage  Storage
	users    UsersClient
	notifier Notifier
	baseURL  string // base para shareableUrl
	log      *slog.Logger
	now      func() time.Time
}

func NewService(repo LinkRepository, storage Storage, users UsersClient, notifier Notifier, baseURL string, log *slog.Logger) *Service {
	return &Service{repo: repo, storage: storage, users: users, notifier: notifier, baseURL: baseURL, log: log, now: time.Now}
}

// CreateLinkOutput é a resposta de POST /links (spec §5).
type CreateLinkOutput struct {
	LinkID    string `json:"linkId"`
	UploadURL string `json:"uploadUrl"`
	ExpiresIn int64  `json:"expiresIn"`
	Status    string `json:"status"`
}

// CreateLink cria o link (LINK_CREATED) e devolve a presigned PUT do S3.
func (s *Service) CreateLink(ctx context.Context, userID, fileName string, isPrivate bool) (*CreateLinkOutput, error) {
	l := link.New(userID, fileName, isPrivate, s.now())
	l.ShareableURL = s.baseURL + "/links/" + l.LinkID

	uploadURL, err := s.storage.PresignPut(ctx, l.S3RawKey, presignExpiry)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, l); err != nil {
		return nil, err
	}
	ev := &link.Event{
		LinkID:     l.LinkID,
		CreatedAt:  s.now().UTC().Format(time.RFC3339Nano),
		StatusFrom: l.Status,
		StatusTo:   l.Status,
		EventType:  "LINK_CREATED",
		ExpiresAt:  l.ExpiresAt,
	}
	if err := s.repo.SaveEvent(ctx, ev); err != nil {
		s.log.Warn("failed to save LINK_CREATED event", "linkId", l.LinkID, "err", err)
	}
	return &CreateLinkOutput{
		LinkID:    l.LinkID,
		UploadURL: uploadURL,
		ExpiresIn: int64(presignExpiry.Seconds()),
		Status:    string(l.Status),
	}, nil
}

// GetLink busca por id, conferindo posse (admins veem tudo).
func (s *Service) GetLink(ctx context.Context, linkID, requesterID, role string) (*link.Link, error) {
	l, err := s.repo.Get(ctx, linkID)
	if err != nil {
		return nil, err
	}
	if role != "administrator" && !l.OwnedBy(requesterID) {
		return nil, link.ErrForbidden
	}
	return l, nil
}

// ListAll lista todos os links (somente administrator — validado no handler).
func (s *Service) ListAll(ctx context.Context) ([]link.Link, error) {
	return s.repo.ListAll(ctx)
}

// ListByUser lista links de um usuário.
func (s *Service) ListByUser(ctx context.Context, targetUserID, requesterID, role string) ([]link.Link, error) {
	if role != "administrator" && targetUserID != requesterID {
		return nil, link.ErrForbidden
	}
	return s.repo.ListByUser(ctx, targetUserID)
}

// ListEvents devolve o histórico de transições (GET /links/:id/events).
func (s *Service) ListEvents(ctx context.Context, linkID, requesterID, role string) ([]link.Event, error) {
	if _, err := s.GetLink(ctx, linkID, requesterID, role); err != nil {
		return nil, err
	}
	return s.repo.ListEvents(ctx, linkID)
}

// ConfirmUploadFromS3Event confirma o upload a partir do evento S3
// ObjectCreated no prefixo {linkId}/raw/ (fan-out via SNS, ver
// video-upload-confirmation-queue): LINK_CREATED/UPLOAD_PENDING ->
// UPLOAD_COMPLETED -> PROCESSING_PENDING. Substitui o antigo callback HTTP
// PUT /links/:id/upload — o próprio S3 confirma a gravação do arquivo, sem
// depender do frontend notificar o backend.
//
// Chamado pelo consumer da fila (não por um usuário autenticado), então não
// há checagem de ownership. Idempotente: entrega at-least-once do SQS pode
// repetir o mesmo evento numa transição já aplicada — mesmo padrão do
// ApplyStatusEvent.
func (s *Service) ConfirmUploadFromS3Event(ctx context.Context, linkID string) error {
	l, err := s.repo.Get(ctx, linkID)
	if err != nil {
		if errors.Is(err, link.ErrNotFound) {
			s.log.Warn("S3 upload event for unknown link, dropping", "linkId", linkID)
			return nil // nunca existirá — não adianta retentar
		}
		return err
	}
	if l.Status != link.StatusLinkCreated && l.Status != link.StatusUploadPending {
		s.log.Info("idempotent skip: upload already confirmed", "linkId", linkID, "status", l.Status)
		return nil
	}
	if err := s.applyTransition(ctx, l, link.StatusUploadCompleted, "S3_UPLOAD_EVENT", nil); err != nil {
		return err
	}
	return s.applyTransition(ctx, l, link.StatusProcessingPending, "S3_UPLOAD_EVENT", nil)
}

// DownloadOutput é a resposta de GET /links/:id/download.
type DownloadOutput struct {
	DownloadURL string `json:"downloadUrl"`
	ExpiresIn   int64  `json:"expiresIn"`
}

// Download devolve presigned GET fresca se status = PROCESSING_COMPLETED (spec §5).
func (s *Service) Download(ctx context.Context, linkID, requesterID, role string) (*DownloadOutput, error) {
	l, err := s.GetLink(ctx, linkID, requesterID, role)
	if err != nil {
		return nil, err
	}
	if l.Status != link.StatusProcessingCompleted || l.S3ProcessedKey == nil {
		return nil, link.ErrInvalidTransition{From: l.Status, To: link.StatusProcessingCompleted}
	}
	url, err := s.storage.PresignGet(ctx, *l.S3ProcessedKey, presignExpiry)
	if err != nil {
		return nil, err
	}
	return &DownloadOutput{DownloadURL: url, ExpiresIn: int64(presignExpiry.Seconds())}, nil
}

// StatusEvent é o contrato de mensagem da video-processing-status-queue (ADR-007).
type StatusEvent struct {
	LinkID         string `json:"linkId"`
	EventType      string `json:"eventType"` // PROCESSING_STARTED | PROCESSING_COMPLETED | PROCESSING_FAILED
	S3ProcessedKey string `json:"s3ProcessedKey,omitempty"`
	Reason         string `json:"reason,omitempty"` // invalid_resolution | max_retries_exceeded
}

// ApplyStatusEvent é chamado pelo consumer SQS. Idempotente: evento repetido
// numa transição já aplicada é ignorado (entrega at-least-once do SQS, spec §9).
func (s *Service) ApplyStatusEvent(ctx context.Context, ev StatusEvent) error {
	l, err := s.repo.Get(ctx, ev.LinkID)
	if err != nil {
		return err
	}
	target := link.Status(ev.EventType)
	if !link.IsValid(target) {
		s.log.Warn("unknown status event", "eventType", ev.EventType, "linkId", ev.LinkID)
		return nil // descarta mensagem malformada — não reprocessar
	}
	if l.Status == target || link.IsTerminal(l.Status) {
		s.log.Info("idempotent skip", "linkId", ev.LinkID, "status", l.Status, "event", ev.EventType)
		return nil
	}
	// worker pode emitir PROCESSING_STARTED com link ainda em UPLOAD_COMPLETED
	// (S3 event chegou antes do callback) — a máquina de estados já permite.
	var meta map[string]any
	if ev.Reason != "" {
		meta = map[string]any{"reason": ev.Reason}
	}
	if ev.S3ProcessedKey != "" {
		l.S3ProcessedKey = &ev.S3ProcessedKey
	}
	if err := s.applyTransition(ctx, l, target, "STATUS_QUEUE", meta); err != nil {
		return err
	}
	if target == link.StatusProcessingFailed {
		s.notifyFailure(ctx, l, ev.Reason) // melhor-esforço (ADR-008)
	}
	return nil
}

// applyTransition centraliza transição + persistência + evento de histórico.
func (s *Service) applyTransition(ctx context.Context, l *link.Link, to link.Status, eventType string, meta map[string]any) error {
	from := l.Status
	ev, err := l.Transition(to, eventType, meta, s.now())
	if err != nil {
		return err
	}
	if err := s.repo.Update(ctx, l, from); err != nil {
		return err
	}
	if err := s.repo.SaveEvent(ctx, ev); err != nil {
		s.log.Warn("failed to save link event", "linkId", l.LinkID, "err", err)
	}
	return nil
}

// notifyFailure resolve o usuário (mock do users-service) e publica no SNS.
// Falha aqui nunca bloqueia a atualização de status — loga e segue (ADR-008).
func (s *Service) notifyFailure(ctx context.Context, l *link.Link, reason string) {
	u, err := s.users.GetUser(ctx, l.UserID)
	if err != nil {
		s.log.Error("failed to resolve user for notification", "userId", l.UserID, "err", err)
		return
	}
	var p NotificationPayload
	p.Recipient.Email = u.Email
	p.Recipient.Name = u.Name
	p.Data.FileName = l.FileName
	p.Data.LinkID = l.LinkID
	p.Data.Reason = reason
	if err := s.notifier.NotifyProcessingFailed(ctx, p); err != nil {
		s.log.Error("failed to publish notification", "linkId", l.LinkID, "err", err)
	}
}
