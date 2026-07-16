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

echo "=== recursos criados ==="
awslocal dynamodb list-tables
awslocal sqs list-queues
awslocal sns list-topics
