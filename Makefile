.PHONY: deps build test run infra-up infra-down token token-admin simulate-worker

deps:
	go mod tidy

build:
	go build ./...

test:
	go vet ./...
	go test ./... -v

# sobe LocalStack + cria tabelas/fila/bucket/tópico
infra-up:
	docker compose -f deploy/localstack/docker-compose.yml up -d
	@echo "aguardando LocalStack inicializar os recursos..."
	@sleep 8

infra-down:
	docker compose -f deploy/localstack/docker-compose.yml down

run:
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/api

token:
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/token -user u-123 -role user

token-admin:
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/token -user admin-1 -role administrator

# simula o processing-worker publicando um evento na status-queue
# uso: make simulate-worker LINK=<linkId> EVENT=PROCESSING_STARTED
simulate-worker:
	AWS_DEFAULT_REGION=us-east-1 AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
	aws --endpoint-url=http://localhost:4566 --region us-east-1 sqs send-message \
		--queue-url http://localhost:4566/000000000000/video-processing-status-queue \
		--message-body '{"linkId":"$(LINK)","eventType":"$(EVENT)","s3ProcessedKey":"$(KEY)","reason":"$(REASON)"}'
