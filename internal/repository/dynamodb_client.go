package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"portfolio-agent/internal/domain"
)

const (
	skPrefixMsg = "MSG#"
	skMeta      = "META#"
	ttlDuration = 30 * 24 * time.Hour // 30-day TTL
)

// dynamodbAPI is the minimal DynamoDB interface required by Client.
// Defined here for testability.
type dynamodbAPI interface {
	GetItem(ctx context.Context, in *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, in *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(ctx context.Context, in *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	TransactWriteItems(ctx context.Context, in *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

// ReadWriter defines the conversation state operations consumed by the handler.
type ReadWriter interface {
	GetConversationTurnCount(ctx context.Context, conversationID string) (int, error)
	GetHistory(ctx context.Context, conversationID string, limit int) ([]domain.Message, error)
	SaveCompletedTurn(ctx context.Context, conversationID, question, answer string, turns int) error
	WriteMessage(ctx context.Context, msg domain.Message) error
	UpsertMeta(ctx context.Context, meta domain.ConversationMeta) error
}

// Client wraps a DynamoDB table for conversation state.
type Client struct {
	api       dynamodbAPI
	tableName string
}

// New creates a new repository Client.
func New(api dynamodbAPI, tableName string) (*Client, error) {
	if api == nil {
		return nil, errors.New("repository: api must not be nil")
	}
	if strings.TrimSpace(tableName) == "" {
		return nil, errors.New("repository: table name must not be empty")
	}
	return &Client{api: api, tableName: tableName}, nil
}

// convPK returns the DynamoDB partition key for a conversation.
func convPK(conversationID string) string {
	return "CONV#" + conversationID
}

// msgSK returns the sort key for a message using the current UTC timestamp.
func msgSK(ts time.Time) string {
	return skPrefixMsg + ts.UTC().Format(time.RFC3339Nano)
}

// ttlValue returns a Unix timestamp 30 days in the future.
func ttlValue() int64 {
	return time.Now().Add(ttlDuration).Unix()
}

// GetHistory queries all MSG# items for a conversation ordered chronologically.
func (c *Client) GetHistory(ctx context.Context, conversationID string, limit int) ([]domain.Message, error) {
	pk := convPK(conversationID)

	in := &dynamodb.QueryInput{
		TableName:              aws.String(c.tableName),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: pk},
			":prefix": &types.AttributeValueMemberS{Value: skPrefixMsg},
		},
		// Read newest first so LIMIT favors the most recent context.
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(int32(limit)),
	}

	out, err := c.api.Query(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("repository: GetHistory query: %w", err)
	}

	msgs := make([]domain.Message, 0, len(out.Items))
	for _, item := range out.Items {
		msg, err := itemToMessage(item)
		if err != nil {
			return nil, fmt.Errorf("repository: GetHistory unmarshal: %w", err)
		}
		msgs = append(msgs, msg)
	}
	// Reverse to chronological order before returning to prompt assembly.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// GetConversationTurnCount returns the persisted successful turn count for a conversation.
func (c *Client) GetConversationTurnCount(ctx context.Context, conversationID string) (int, error) {
	out, err := c.api.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(c.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: convPK(conversationID)},
			"SK": &types.AttributeValueMemberS{Value: skMeta},
		},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return 0, fmt.Errorf("repository: GetConversationTurnCount get item: %w", err)
	}
	if out == nil || len(out.Item) == 0 {
		return 0, nil
	}

	turns, err := intAttr(out.Item, "turns")
	if err != nil {
		return 0, fmt.Errorf("repository: GetConversationTurnCount decode turns: %w", err)
	}
	return turns, nil
}

// WriteMessage persists a new message record with status=pending.
func (c *Client) WriteMessage(ctx context.Context, msg domain.Message) error {
	if msg.PK == "" || msg.SK == "" {
		return errors.New("repository: WriteMessage: PK and SK are required")
	}

	_, err := c.api.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(c.tableName),
		Item:                messageItem(msg),
		ConditionExpression: aws.String("attribute_not_exists(PK) AND attribute_not_exists(SK)"),
	})
	if err != nil {
		return fmt.Errorf("repository: WriteMessage: %w", err)
	}
	return nil
}

// UpsertMeta writes or replaces the conversation metadata record.
func (c *Client) UpsertMeta(ctx context.Context, meta domain.ConversationMeta) error {
	_, err := c.api.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.tableName),
		Item:      metaItem(meta),
	})
	if err != nil {
		return fmt.Errorf("repository: UpsertMeta: %w", err)
	}
	return nil
}

// SaveTurn writes the completed message and updated metadata in one transaction.
func (c *Client) SaveTurn(ctx context.Context, msg domain.Message, meta domain.ConversationMeta) error {
	if msg.PK == "" || msg.SK == "" {
		return errors.New("repository: SaveTurn: message PK and SK are required")
	}
	if meta.PK == "" || meta.SK == "" {
		return errors.New("repository: SaveTurn: meta PK and SK are required")
	}

	_, err := c.api.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Put: &types.Put{
					TableName:           aws.String(c.tableName),
					Item:                messageItem(msg),
					ConditionExpression: aws.String("attribute_not_exists(PK) AND attribute_not_exists(SK)"),
				},
			},
			{
				Put: &types.Put{
					TableName: aws.String(c.tableName),
					Item:      metaItem(meta),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("repository: SaveTurn: %w", err)
	}
	return nil
}

// SaveCompletedTurn persists the successful user turn and updates metadata.
func (c *Client) SaveCompletedTurn(ctx context.Context, conversationID, question, answer string, turns int) error {
	msg := NewMessage(conversationID, question, 0, "complete")
	msg.Answer = answer
	meta := NewConversationMeta(conversationID, turns)
	if err := c.SaveTurn(ctx, msg, meta); err != nil {
		return fmt.Errorf("repository: SaveCompletedTurn: %w", err)
	}
	return nil
}

// NewMessage constructs a Message with PK/SK/TTL set from conversationID and current time.
func NewMessage(conversationID, text string, tokens int, status string) domain.Message {
	now := time.Now().UTC()
	return domain.Message{
		PK:             convPK(conversationID),
		SK:             msgSK(now),
		ConversationID: conversationID,
		Text:           text,
		Tokens:         tokens,
		Status:         status,
		TTL:            ttlValue(),
	}
}

// NewConversationMeta constructs a ConversationMeta record.
func NewConversationMeta(conversationID string, turns int) domain.ConversationMeta {
	return domain.ConversationMeta{
		PK:             convPK(conversationID),
		SK:             skMeta,
		ConversationID: conversationID,
		LastActivity:   time.Now().UTC().Format(time.RFC3339),
		Turns:          turns,
		TTL:            ttlValue(),
	}
}

// itemToMessage converts a DynamoDB attribute map to a Message.
func itemToMessage(item map[string]types.AttributeValue) (domain.Message, error) {
	pk, err := strAttr(item, "PK")
	if err != nil {
		return domain.Message{}, err
	}
	sk, err := strAttr(item, "SK")
	if err != nil {
		return domain.Message{}, err
	}
	text, err := strAttr(item, "text")
	if err != nil {
		return domain.Message{}, err
	}
	answer, _ := strAttr(item, "answer") // allow empty
	status, _ := strAttr(item, "status") // allow empty

	return domain.Message{
		PK:     pk,
		SK:     sk,
		Text:   text,
		Answer: answer,
		Status: status,
	}, nil
}

func messageItem(msg domain.Message) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK":             &types.AttributeValueMemberS{Value: msg.PK},
		"SK":             &types.AttributeValueMemberS{Value: msg.SK},
		"conversationId": &types.AttributeValueMemberS{Value: msg.ConversationID},
		"text":           &types.AttributeValueMemberS{Value: msg.Text},
		"answer":         &types.AttributeValueMemberS{Value: msg.Answer},
		"tokens":         &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", msg.Tokens)},
		"status":         &types.AttributeValueMemberS{Value: msg.Status},
		"ttl":            &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", msg.TTL)},
	}
}

func metaItem(meta domain.ConversationMeta) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK":             &types.AttributeValueMemberS{Value: meta.PK},
		"SK":             &types.AttributeValueMemberS{Value: meta.SK},
		"conversationId": &types.AttributeValueMemberS{Value: meta.ConversationID},
		"lastActivity":   &types.AttributeValueMemberS{Value: meta.LastActivity},
		"turns":          &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", meta.Turns)},
		"ttl":            &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", meta.TTL)},
	}
}

func strAttr(item map[string]types.AttributeValue, key string) (string, error) {
	v, ok := item[key]
	if !ok {
		return "", fmt.Errorf("repository: missing attribute %q", key)
	}
	s, ok := v.(*types.AttributeValueMemberS)
	if !ok {
		return "", fmt.Errorf("repository: attribute %q is not a string", key)
	}
	return s.Value, nil
}

func intAttr(item map[string]types.AttributeValue, key string) (int, error) {
	v, ok := item[key]
	if !ok {
		return 0, fmt.Errorf("repository: missing attribute %q", key)
	}
	n, ok := v.(*types.AttributeValueMemberN)
	if !ok {
		return 0, fmt.Errorf("repository: attribute %q is not a number", key)
	}
	parsed, err := strconv.Atoi(n.Value)
	if err != nil {
		return 0, fmt.Errorf("repository: parse attribute %q: %w", key, err)
	}
	return parsed, nil
}
