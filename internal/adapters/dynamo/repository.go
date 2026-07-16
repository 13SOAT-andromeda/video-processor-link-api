// Package dynamo implementa app.LinkRepository sobre as tabelas
// Links e LinkEvents (spec §4). TTL nativo via expiresAt.
package dynamo

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/fiap/links-service/internal/domain/link"
)

const userIndex = "userId-index" // GSI para query por usuário (spec §4)

type Repository struct {
	db          *dynamodb.Client
	linksTable  string
	eventsTable string
}

func NewRepository(db *dynamodb.Client, linksTable, eventsTable string) *Repository {
	return &Repository{db: db, linksTable: linksTable, eventsTable: eventsTable}
}

func (r *Repository) Save(ctx context.Context, l *link.Link) error {
	item, err := attributevalue.MarshalMap(l)
	if err != nil {
		return err
	}
	_, err = r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(r.linksTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(linkId)"),
	})
	return err
}

// Update usa optimistic locking pelo status esperado — protege contra
// consumo concorrente da status-queue aplicando transições fora de ordem.
func (r *Repository) Update(ctx context.Context, l *link.Link, expectedStatus link.Status) error {
	item, err := attributevalue.MarshalMap(l)
	if err != nil {
		return err
	}
	_, err = r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(r.linksTable),
		Item:                item,
		ConditionExpression: aws.String("#st = :expected"),
		ExpressionAttributeNames: map[string]string{
			"#st": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":expected": &types.AttributeValueMemberS{Value: string(expectedStatus)},
		},
	})
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) {
		return link.ErrInvalidTransition{From: expectedStatus, To: l.Status}
	}
	return err
}

func (r *Repository) Get(ctx context.Context, linkID string) (*link.Link, error) {
	out, err := r.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.linksTable),
		Key: map[string]types.AttributeValue{
			"linkId": &types.AttributeValueMemberS{Value: linkID},
		},
	})
	if err != nil {
		return nil, err
	}
	if out.Item == nil {
		return nil, link.ErrNotFound
	}
	var l link.Link
	if err := attributevalue.UnmarshalMap(out.Item, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *Repository) ListAll(ctx context.Context) ([]link.Link, error) {
	out, err := r.db.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(r.linksTable)})
	if err != nil {
		return nil, err
	}
	links := []link.Link{}
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &links); err != nil {
		return nil, err
	}
	return links, nil
}

func (r *Repository) ListByUser(ctx context.Context, userID string) ([]link.Link, error) {
	out, err := r.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(r.linksTable),
		IndexName:              aws.String(userIndex),
		KeyConditionExpression: aws.String("userId = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, err
	}
	links := []link.Link{}
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &links); err != nil {
		return nil, err
	}
	return links, nil
}

func (r *Repository) SaveEvent(ctx context.Context, ev *link.Event) error {
	item, err := attributevalue.MarshalMap(ev)
	if err != nil {
		return err
	}
	_, err = r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.eventsTable),
		Item:      item,
	})
	return err
}

func (r *Repository) ListEvents(ctx context.Context, linkID string) ([]link.Event, error) {
	out, err := r.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(r.eventsTable),
		KeyConditionExpression: aws.String("linkId = :id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":id": &types.AttributeValueMemberS{Value: linkID},
		},
		ScanIndexForward: aws.Bool(true), // ordem cronológica (SK = createdAt)
	})
	if err != nil {
		return nil, err
	}
	events := []link.Event{}
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &events); err != nil {
		return nil, err
	}
	return events, nil
}
