// Package queue implementa o consumer contínuo (goroutine) da
// video-processing-status-queue — a razão de o links-service rodar em pod
// EKS e não em Lambda (ADR-011). Sem DLQ própria: erros de consumo são
// tratados internamente (ADR-003, adendo v5).
package queue

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/fiap/links-service/internal/app"
)

type Consumer struct {
	client   *sqs.Client
	queueURL string
	svc      *app.Service
	log      *slog.Logger
}

func NewConsumer(client *sqs.Client, queueURL string, svc *app.Service, log *slog.Logger) *Consumer {
	return &Consumer{client: client, queueURL: queueURL, svc: svc, log: log}
}

// Run faz long polling até o contexto ser cancelado.
func (c *Consumer) Run(ctx context.Context) {
	c.log.Info("status-queue consumer started", "queue", c.queueURL)
	for {
		select {
		case <-ctx.Done():
			c.log.Info("status-queue consumer stopped")
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

func (c *Consumer) handle(ctx context.Context, body, receipt *string) {
	if body == nil {
		return
	}
	var ev app.StatusEvent
	if err := json.Unmarshal([]byte(*body), &ev); err != nil {
		c.log.Warn("malformed status event, dropping", "body", *body, "err", err)
		c.delete(ctx, receipt)
		return
	}
	if err := c.svc.ApplyStatusEvent(ctx, ev); err != nil {
		// não deleta — a mensagem volta para a fila após o visibility timeout
		c.log.Error("failed to apply status event, will retry", "linkId", ev.LinkID, "err", err)
		return
	}
	c.delete(ctx, receipt)
}

func (c *Consumer) delete(ctx context.Context, receipt *string) {
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
