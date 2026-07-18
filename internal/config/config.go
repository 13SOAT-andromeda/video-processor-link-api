package config

import (
	"os"
	"strconv"
)

// Config carrega tudo de variáveis de ambiente (ver .env.example).
type Config struct {
	Port            string
	BaseURL         string
	JWTSecret       string
	AWSEndpoint     string // vazio = AWS real; apontar para LocalStack em dev
	AWSRegion       string
	LinksTable      string
	EventsTable     string
	VideoBucket     string
	StatusQueueURL  string
	NotificationARN string
	UseUserSvcMock  bool   // true = mock; false = HTTP no users-service do cluster
	UsersBaseURL    string // usado quando UseUserSvcMock=false
}

func Load() Config {
	return Config{
		Port:            getenv("PORT", "8080"),
		BaseURL:         getenv("BASE_URL", "http://localhost:8080"),
		JWTSecret:       getenv("JWT_SECRET", "dev-secret-change-me"),
		AWSEndpoint:     os.Getenv("AWS_ENDPOINT_URL"),
		AWSRegion:       getenv("AWS_REGION", "us-east-1"),
		LinksTable:      getenv("DYNAMO_LINKS_TABLE", "Links"),
		EventsTable:     getenv("DYNAMO_EVENTS_TABLE", "LinkEvents"),
		VideoBucket:     getenv("S3_BUCKET", "video-processing-bucket"),
		StatusQueueURL:  os.Getenv("STATUS_QUEUE_URL"),
		NotificationARN: os.Getenv("NOTIFICATION_TOPIC_ARN"),
		UseUserSvcMock:  getbool("USE_USER_SVC_MOCK", true),
		UsersBaseURL:    getenv("USERS_BASE_URL", "http://video-processor-users-api-svc.default.svc.cluster.local"),
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
