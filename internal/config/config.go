package config

import (
	"os"
	"strconv"
)

// Config carrega tudo de variáveis de ambiente (ver .env.example).
type Config struct {
	Port                       string
	BaseURL                    string
	JWTSecret                  string
	AWSEndpoint                string // vazio = AWS real; apontar para LocalStack em dev
	AWSRegion                  string
	LinksTable                 string
	EventsTable                string
	VideoBucket                string
	StatusQueueURL             string
	NotificationARN            string
	UploadConfirmationQueueURL string // vazio = consumer desabilitado
	UseUserSvcMock             bool   // true = mock; false = HTTP no users-service do cluster
	UsersBaseURL               string // usado quando UseUserSvcMock=false

	// Datadog (APM) — tracer só é iniciado se DDAgentHost não estiver vazio,
	// mesmo padrão de "vazio = desligado" já usado por StatusQueueURL/NotificationARN.
	DDAgentHost string
	DDService   string
	DDEnv       string
	DDVersion   string
}

func Load() Config {
	return Config{
		Port:                       getenv("PORT", "8080"),
		BaseURL:                    getenv("BASE_URL", "http://localhost:8080"),
		JWTSecret:                  getenv("JWT_SECRET", "dev-secret-change-me"),
		AWSEndpoint:                os.Getenv("AWS_ENDPOINT_URL"),
		AWSRegion:                  getenv("AWS_REGION", "us-east-1"),
		LinksTable:                 getenv("DYNAMO_LINKS_TABLE", "Links"),
		EventsTable:                getenv("DYNAMO_EVENTS_TABLE", "LinkEvents"),
		VideoBucket:                getenv("S3_BUCKET", "video-processing-bucket"),
		StatusQueueURL:             os.Getenv("STATUS_QUEUE_URL"),
		NotificationARN:            os.Getenv("NOTIFICATION_TOPIC_ARN"),
		UploadConfirmationQueueURL: os.Getenv("UPLOAD_CONFIRMATION_QUEUE_URL"),
		UseUserSvcMock:             getbool("USE_USER_SVC_MOCK", true),
		UsersBaseURL:               getenv("USERS_BASE_URL", "http://video-processor-users-api-svc.default.svc.cluster.local"),

		DDAgentHost: os.Getenv("DD_AGENT_HOST"),
		DDService:   getenv("DD_SERVICE", "video-processor-link-api"),
		DDEnv:       getenv("DD_ENV", "dev"),
		DDVersion:   getenv("DD_VERSION", "dev"),
	}
}

func getbool(k string, def bool) bool {
	v, err := strconv.ParseBool(os.Getenv(k))
	if err != nil {
		return def
	}
	return v
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
