// Package notification publica no SNS notification-topic seguindo o contrato
// confirmado do notification-service (ADR-008): templateType em
// MessageAttributes, recipient já resolvido no corpo.
package notification

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/fiap/links-service/internal/app"
)

type SNSNotifier struct {
	client   *sns.Client
	topicARN string
}

func NewSNSNotifier(client *sns.Client, topicARN string) *SNSNotifier {
	return &SNSNotifier{client: client, topicARN: topicARN}
}

func (n *SNSNotifier) NotifyProcessingFailed(ctx context.Context, p app.NotificationPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = n.client.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(n.topicARN),
		Message:  aws.String(string(body)),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"templateType": {
				DataType:    aws.String("String"),
				StringValue: aws.String("PROCESSING_FAILED"),
			},
		},
	})
	return err
}

// NoopNotifier é usado quando NOTIFICATION_TOPIC_ARN não está configurado.
type NoopNotifier struct{}

func (NoopNotifier) NotifyProcessingFailed(_ context.Context, _ app.NotificationPayload) error {
	return nil
}
