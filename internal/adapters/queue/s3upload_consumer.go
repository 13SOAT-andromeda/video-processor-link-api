package queue

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/fiap/links-service/internal/app"
)

// S3UploadConsumer consome a video-upload-confirmation-queue — fan-out SNS
// (raw_message_delivery) do evento S3 ObjectCreated no prefixo {linkId}/raw/
// — e confirma o upload sem depender do frontend chamar de volta o backend.
// Substitui o antigo PUT /links/:id/upload.
type S3UploadConsumer struct {
	client   *sqs.Client
	queueURL string
	svc      *app.Service
	log      *slog.Logger
}

func NewS3UploadConsumer(client *sqs.Client, queueURL string, svc *app.Service, log *slog.Logger) *S3UploadConsumer {
	return &S3UploadConsumer{client: client, queueURL: queueURL, svc: svc, log: log}
}

// Run faz long polling até o contexto ser cancelado.
func (c *S3UploadConsumer) Run(ctx context.Context) {
	c.log.Info("s3-upload consumer started", "queue", c.queueURL)
	for {
		select {
		case <-ctx.Done():
			c.log.Info("s3-upload consumer stopped")
			return
		default:
		}
		out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20, // long polling
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log.Error("receive message failed", "err", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for _, msg := range out.Messages {
			c.handle(ctx, msg.Body, msg.ReceiptHandle)
		}
	}
}

// s3Event é o subconjunto do payload de notificação do S3 que interessa aqui
// (Records[].s3.object.key). O evento s3:TestEvent, enviado automaticamente
// quando a notification config é criada, não tem campo Records — cai no loop
// vazio abaixo sem tratamento especial.
type s3Event struct {
	Records []struct {
		S3 struct {
			Object struct {
				Key string `json:"key"`
			} `json:"object"`
		} `json:"s3"`
	} `json:"Records"`
}

func (c *S3UploadConsumer) handle(ctx context.Context, body, receipt *string) {
	if body == nil {
		return
	}
	var ev s3Event
	if err := json.Unmarshal([]byte(*body), &ev); err != nil {
		c.log.Warn("malformed S3 event, dropping", "body", *body, "err", err)
		c.delete(ctx, receipt)
		return
	}
	allOK := true
	for _, rec := range ev.Records {
		linkID, ok := parseRawUploadKey(rec.S3.Object.Key)
		if !ok {
			c.log.Info("non-raw-upload key, skipping", "key", rec.S3.Object.Key)
			continue
		}
		if err := c.svc.ConfirmUploadFromS3Event(ctx, linkID); err != nil {
			c.log.Error("failed to confirm upload from S3 event, will retry", "linkId", linkID, "err", err)
			allOK = false
		}
	}
	if allOK {
		c.delete(ctx, receipt)
	}
	// se algum record falhou, não deleta — a mensagem volta pra fila após o
	// visibility timeout (mesmo padrão do consumer da status-queue)
}

// parseRawUploadKey extrai o linkId de uma chave {linkId}/raw/{fileName} —
// o único formato que o filtro .mp4 da notification deveria entregar aqui.
func parseRawUploadKey(rawKey string) (linkID string, ok bool) {
	key, err := url.QueryUnescape(rawKey)
	if err != nil {
		key = rawKey
	}
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] != "raw" {
		return "", false
	}
	return parts[0], true
}

func (c *S3UploadConsumer) delete(ctx context.Context, receipt *string) {
	if receipt == nil {
		return
	}
	if _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.queueURL),
		ReceiptHandle: receipt,
	}); err != nil {
		c.log.Error("failed to delete message", "err", err)
	}
}
