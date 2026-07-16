package app

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fiap/links-service/internal/domain/link"
)

// ---- fakes in-memory ----

type fakeRepo struct {
	links  map[string]*link.Link
	events []link.Event
}

func newFakeRepo() *fakeRepo { return &fakeRepo{links: map[string]*link.Link{}} }

func (f *fakeRepo) Save(_ context.Context, l *link.Link) error {
	cp := *l
	f.links[l.LinkID] = &cp
	return nil
}
func (f *fakeRepo) Update(_ context.Context, l *link.Link, expected link.Status) error {
	cur, ok := f.links[l.LinkID]
	if !ok {
		return link.ErrNotFound
	}
	if cur.Status != expected {
		return link.ErrInvalidTransition{From: expected, To: l.Status}
	}
	cp := *l
	f.links[l.LinkID] = &cp
	return nil
}
func (f *fakeRepo) Get(_ context.Context, id string) (*link.Link, error) {
	l, ok := f.links[id]
	if !ok {
		return nil, link.ErrNotFound
	}
	cp := *l
	return &cp, nil
}
func (f *fakeRepo) ListAll(_ context.Context) ([]link.Link, error) {
	out := []link.Link{}
	for _, l := range f.links {
		out = append(out, *l)
	}
	return out, nil
}
func (f *fakeRepo) ListByUser(_ context.Context, uid string) ([]link.Link, error) {
	out := []link.Link{}
	for _, l := range f.links {
		if l.UserID == uid {
			out = append(out, *l)
		}
	}
	return out, nil
}
func (f *fakeRepo) SaveEvent(_ context.Context, ev *link.Event) error {
	f.events = append(f.events, *ev)
	return nil
}
func (f *fakeRepo) ListEvents(_ context.Context, id string) ([]link.Event, error) {
	out := []link.Event{}
	for _, ev := range f.events {
		if ev.LinkID == id {
			out = append(out, ev)
		}
	}
	return out, nil
}

type fakeStorage struct{}

func (fakeStorage) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://s3.mock/put/" + key, nil
}
func (fakeStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://s3.mock/get/" + key, nil
}

type fakeUsers struct{}

func (fakeUsers) GetUser(_ context.Context, id string) (*User, error) {
	return &User{ID: id, Name: "Test", Email: "test@mock.local"}, nil
}

type fakeNotifier struct{ published []NotificationPayload }

func (f *fakeNotifier) NotifyProcessingFailed(_ context.Context, p NotificationPayload) error {
	f.published = append(f.published, p)
	return nil
}

func newTestService() (*Service, *fakeRepo, *fakeNotifier) {
	repo := newFakeRepo()
	notifier := &fakeNotifier{}
	svc := NewService(repo, fakeStorage{}, fakeUsers{}, notifier, "http://localhost:8080", slog.Default())
	return svc, repo, notifier
}

// ---- testes ----

func TestCreateLink(t *testing.T) {
	svc, repo, _ := newTestService()
	out, err := svc.CreateLink(context.Background(), "u-1", "video.mp4", false)
	require.NoError(t, err)
	assert.Equal(t, "LINK_CREATED", out.Status)
	assert.Contains(t, out.UploadURL, out.LinkID+"/raw/video.mp4")
	assert.Len(t, repo.events, 1)
}

func TestOwnershipEnforced(t *testing.T) {
	svc, _, _ := newTestService()
	out, _ := svc.CreateLink(context.Background(), "u-1", "v.mp4", false)

	_, err := svc.GetLink(context.Background(), out.LinkID, "u-2", "user")
	assert.ErrorIs(t, err, link.ErrForbidden)

	_, err = svc.GetLink(context.Background(), out.LinkID, "u-2", "administrator")
	assert.NoError(t, err, "administrator vê qualquer link")

	_, err = svc.ListByUser(context.Background(), "u-1", "u-2", "user")
	assert.ErrorIs(t, err, link.ErrForbidden)
}

func TestConfirmUploadHappyPath(t *testing.T) {
	svc, _, _ := newTestService()
	out, _ := svc.CreateLink(context.Background(), "u-1", "v.mp4", false)

	l, err := svc.ConfirmUpload(context.Background(), out.LinkID, "u-1", "user")
	require.NoError(t, err)
	assert.Equal(t, link.StatusProcessingPending, l.Status)

	// callback repetido -> 409
	_, err = svc.ConfirmUpload(context.Background(), out.LinkID, "u-1", "user")
	var invalid link.ErrInvalidTransition
	assert.ErrorAs(t, err, &invalid)
}

func TestProcessingFlowAndDownload(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	out, _ := svc.CreateLink(ctx, "u-1", "v.mp4", false)
	_, err := svc.ConfirmUpload(ctx, out.LinkID, "u-1", "user")
	require.NoError(t, err)

	// download antes de completar -> 409
	_, err = svc.Download(ctx, out.LinkID, "u-1", "user")
	assert.Error(t, err)

	require.NoError(t, svc.ApplyStatusEvent(ctx, StatusEvent{LinkID: out.LinkID, EventType: "PROCESSING_STARTED"}))
	require.NoError(t, svc.ApplyStatusEvent(ctx, StatusEvent{
		LinkID: out.LinkID, EventType: "PROCESSING_COMPLETED", S3ProcessedKey: out.LinkID + "/processed/v.zip",
	}))

	dl, err := svc.Download(ctx, out.LinkID, "u-1", "user")
	require.NoError(t, err)
	assert.Contains(t, dl.DownloadURL, "/processed/v.zip")
}

func TestApplyStatusEventIdempotent(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()
	out, _ := svc.CreateLink(ctx, "u-1", "v.mp4", false)
	_, _ = svc.ConfirmUpload(ctx, out.LinkID, "u-1", "user")

	require.NoError(t, svc.ApplyStatusEvent(ctx, StatusEvent{LinkID: out.LinkID, EventType: "PROCESSING_STARTED"}))
	eventsBefore := len(repo.events)

	// entrega duplicada (at-least-once) não gera novo evento nem erro
	require.NoError(t, svc.ApplyStatusEvent(ctx, StatusEvent{LinkID: out.LinkID, EventType: "PROCESSING_STARTED"}))
	assert.Len(t, repo.events, eventsBefore)
}

func TestProcessingFailedTriggersNotification(t *testing.T) {
	svc, _, notifier := newTestService()
	ctx := context.Background()
	out, _ := svc.CreateLink(ctx, "u-1", "v.mp4", false)
	_, _ = svc.ConfirmUpload(ctx, out.LinkID, "u-1", "user")
	require.NoError(t, svc.ApplyStatusEvent(ctx, StatusEvent{LinkID: out.LinkID, EventType: "PROCESSING_STARTED"}))

	require.NoError(t, svc.ApplyStatusEvent(ctx, StatusEvent{
		LinkID: out.LinkID, EventType: "PROCESSING_FAILED", Reason: "max_retries_exceeded",
	}))

	require.Len(t, notifier.published, 1)
	p := notifier.published[0]
	assert.Equal(t, "test@mock.local", p.Recipient.Email)
	assert.Equal(t, "v.mp4", p.Data.FileName)
	assert.Equal(t, "max_retries_exceeded", p.Data.Reason)

	// evento em estado terminal é ignorado, sem nova notificação
	require.NoError(t, svc.ApplyStatusEvent(ctx, StatusEvent{
		LinkID: out.LinkID, EventType: "PROCESSING_FAILED", Reason: "max_retries_exceeded",
	}))
	assert.Len(t, notifier.published, 1)
}

func TestUnknownEventDropped(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	out, _ := svc.CreateLink(ctx, "u-1", "v.mp4", false)
	assert.NoError(t, svc.ApplyStatusEvent(ctx, StatusEvent{LinkID: out.LinkID, EventType: "GARBAGE"}))
}
