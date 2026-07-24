#!/bin/bash
# Bootstrap dos recursos AWS no LocalStack (roda automaticamente no ready.d).
set -euo pipefail
export AWS_DEFAULT_REGION=us-east-1

awslocal dynamodb create-table \
  --table-name Links \
  --attribute-definitions \
    AttributeName=linkId,AttributeType=S \
    AttributeName=userId,AttributeType=S \
  --key-schema AttributeName=linkId,KeyType=HASH \
  --global-secondary-indexes '[{
    "IndexName": "userId-index",
    "KeySchema": [{"AttributeName":"userId","KeyType":"HASH"}],
    "Projection": {"ProjectionType":"ALL"}
  }]' \
  --billing-mode PAY_PER_REQUEST

awslocal dynamodb update-time-to-live \
  --table-name Links \
  --time-to-live-specification "Enabled=true, AttributeName=expiresAt"

awslocal dynamodb create-table \
  --table-name LinkEvents \
  --attribute-definitions \
    AttributeName=linkId,AttributeType=S \
    AttributeName=createdAt,AttributeType=S \
  --key-schema \
    AttributeName=linkId,KeyType=HASH \
    AttributeName=createdAt,KeyType=RANGE \
  --billing-mode PAY_PER_REQUEST

awslocal dynamodb update-time-to-live \
  --table-name LinkEvents \
  --time-to-live-specification "Enabled=true, AttributeName=expiresAt"

awslocal s3 mb s3://video-processing-bucket

awslocal sqs create-queue --queue-name video-processing-status-queue

awslocal sns create-topic --name notification-topic

# fila de confirmação de upload + notification direta do bucket (sem SNS no
# meio — localmente não existe um segundo consumidor do mesmo evento
# disputando o filtro de sufixo, então o fan-out via SNS usado em prod/dev
# real não é necessário aqui). Testa o fluxo PUT presigned -> S3 event -> SQS
# -> links-service confirma o upload de ponta a ponta, sem precisar de
# `make simulate-worker` pra essa etapa.
awslocal sqs create-queue --queue-name video-upload-confirmation-queue

UPLOAD_QUEUE_ARN=$(awslocal sqs get-queue-attributes \
  --queue-url http://localhost:4566/000000000000/video-upload-confirmation-queue \
  --attribute-names QueueArn --query 'Attributes.QueueArn' --output text)

awslocal s3api put-bucket-notification-configuration \
  --bucket video-processing-bucket \
  --notification-configuration "{
    \"QueueConfigurations\": [{
      \"QueueArn\": \"${UPLOAD_QUEUE_ARN}\",
      \"Events\": [\"s3:ObjectCreated:*\"],
      \"Filter\": {\"Key\": {\"FilterRules\": [{\"Name\": \"suffix\", \"Value\": \".mp4\"}]}}
    }]
  }"

echo "=== recursos criados ==="
awslocal dynamodb list-tables
awslocal sqs list-queues
awslocal sns list-topics
