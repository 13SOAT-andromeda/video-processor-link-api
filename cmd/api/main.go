// links-service — API + consumer da video-processing-status-queue.
// Fonte única da verdade do domínio de links (ADR-007).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	ddaws "gopkg.in/DataDog/dd-trace-go.v1/contrib/aws/aws-sdk-go-v2/aws"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"

	"github.com/fiap/links-service/internal/adapters/dynamo"
	"github.com/fiap/links-service/internal/adapters/httpapi"
	"github.com/fiap/links-service/internal/adapters/notification"
	"github.com/fiap/links-service/internal/adapters/queue"
	"github.com/fiap/links-service/internal/adapters/storage"
	"github.com/fiap/links-service/internal/adapters/users"
	"github.com/fiap/links-service/internal/app"
	"github.com/fiap/links-service/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Datadog APM — só inicia se DD_AGENT_HOST estiver configurado (mesmo
	// padrão "vazio = desligado" já usado por STATUS_QUEUE_URL/NOTIFICATION_TOPIC_ARN).
	// Em produção o agent roda como DaemonSet no EKS (iac-video-processor-infra);
	// DD_AGENT_HOST é injetado via Downward API (status.hostIP) no manifest do pod.
	if cfg.DDAgentHost != "" {
		tracer.Start(
			tracer.WithAgentAddr(cfg.DDAgentHost+":8126"),
			tracer.WithService(cfg.DDService),
			tracer.WithEnv(cfg.DDEnv),
			tracer.WithServiceVersion(cfg.DDVersion),
		)
		defer tracer.Stop()
		log.Info("datadog APM enabled", "agentAddr", cfg.DDAgentHost+":8126", "service", cfg.DDService, "env", cfg.DDEnv)
	} else {
		log.Info("datadog APM disabled (DD_AGENT_HOST não configurada)")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWSRegion))
	if err != nil {
		log.Error("failed to load AWS config", "err", err)
		os.Exit(1)
	}
	if cfg.DDAgentHost != "" {
		// instrumenta dynamodb/s3/sqs/sns — cada chamada AWS SDK vira um span filho
		// do span HTTP (rota) ou do span do consumer da status-queue.
		ddaws.AppendMiddleware(&awsCfg)
	}

	// AWS_ENDPOINT_URL definido = LocalStack (dev local)
	dynamoClient := dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
		if cfg.AWSEndpoint != "" {
			o.BaseEndpoint = &cfg.AWSEndpoint
		}
	})
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.AWSEndpoint != "" {
			o.BaseEndpoint = &cfg.AWSEndpoint
			o.UsePathStyle = true // LocalStack não resolve virtual-host style
		}
	})
	sqsClient := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.AWSEndpoint != "" {
			o.BaseEndpoint = &cfg.AWSEndpoint
		}
	})
	snsClient := sns.NewFromConfig(awsCfg, func(o *sns.Options) {
		if cfg.AWSEndpoint != "" {
			o.BaseEndpoint = &cfg.AWSEndpoint
		}
	})

	repo := dynamo.NewRepository(dynamoClient, cfg.LinksTable, cfg.EventsTable)
	store := storage.NewS3Storage(s3Client, cfg.VideoBucket)

	var usersClient app.UsersClient
	if cfg.UseUserSvcMock {
		usersClient = users.NewMockClient()
		log.Info("users client: mock (USE_USER_SVC_MOCK=true)")
	} else {
		usersClient = users.NewHTTPClient(cfg.UsersBaseURL, cfg.JWTSecret)
		log.Info("users client: http", "baseURL", cfg.UsersBaseURL)
	}

	var notifier app.Notifier = notification.NoopNotifier{}
	if cfg.NotificationARN != "" {
		notifier = notification.NewSNSNotifier(snsClient, cfg.NotificationARN)
	}

	svc := app.NewService(repo, store, usersClient, notifier, cfg.BaseURL, log)

	// consumer contínuo da status-queue (goroutine — ADR-011)
	if cfg.StatusQueueURL != "" {
		consumer := queue.NewConsumer(sqsClient, cfg.StatusQueueURL, svc, log)
		go consumer.Run(ctx)
	} else {
		log.Warn("STATUS_QUEUE_URL não configurada — consumer desabilitado")
	}

	ddServiceName := ""
	if cfg.DDAgentHost != "" {
		ddServiceName = cfg.DDService
	}
	router := httpapi.Router(svc, cfg.JWTSecret, ddServiceName)
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router}

	go func() {
		log.Info("links-service listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("shutdown complete")
}
