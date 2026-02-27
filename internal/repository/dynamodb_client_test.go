package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/require"

	"portfolio-agent/internal/domain"
)

type fakeDynamo struct {
	getOut       *dynamodb.GetItemOutput
	getErr       error
	putErr       error
	queryOut     *dynamodb.QueryOutput
	queryErr     error
	txErr        error
	lastGetInput *dynamodb.GetItemInput
	lastPutInput *dynamodb.PutItemInput
	lastQueryIn  *dynamodb.QueryInput
	lastTxInput  *dynamodb.TransactWriteItemsInput
}

func (f *fakeDynamo) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	f.lastGetInput = in
	return f.getOut, f.getErr
}

func (f *fakeDynamo) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.lastPutInput = in
	return &dynamodb.PutItemOutput{}, f.putErr
}

func (f *fakeDynamo) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.lastQueryIn = in
	return f.queryOut, f.queryErr
}

func (f *fakeDynamo) TransactWriteItems(_ context.Context, in *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	f.lastTxInput = in
	return &dynamodb.TransactWriteItemsOutput{}, f.txErr
}

func makeItem(pk, sk, text, answer, status string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK":     &types.AttributeValueMemberS{Value: pk},
		"SK":     &types.AttributeValueMemberS{Value: sk},
		"text":   &types.AttributeValueMemberS{Value: text},
		"answer": &types.AttributeValueMemberS{Value: answer},
		"status": &types.AttributeValueMemberS{Value: status},
	}
}

func makeMetaItem(pk string, turns int) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK":    &types.AttributeValueMemberS{Value: pk},
		"SK":    &types.AttributeValueMemberS{Value: skMeta},
		"turns": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", turns)},
	}
}

func mustNewClient(t *testing.T, db *fakeDynamo) *Client {
	t.Helper()
	c, err := New(db, "test-table")
	require.NoError(t, err)
	return c
}

func TestGetConversationTurnCount_HappyPath(t *testing.T) {
	db := &fakeDynamo{getOut: &dynamodb.GetItemOutput{Item: makeMetaItem("CONV#abc", 7)}}
	c := mustNewClient(t, db)
	turns, err := c.GetConversationTurnCount(context.Background(), "abc")
	require.NoError(t, err)
	require.Equal(t, 7, turns)
	require.NotNil(t, db.lastGetInput)
}

func TestGetConversationTurnCount_MissingMeta(t *testing.T) {
	db := &fakeDynamo{getOut: &dynamodb.GetItemOutput{}}
	c := mustNewClient(t, db)
	turns, err := c.GetConversationTurnCount(context.Background(), "abc")
	require.NoError(t, err)
	require.Equal(t, 0, turns)
}

func TestGetConversationTurnCount_GetItemError(t *testing.T) {
	db := &fakeDynamo{getErr: errors.New("boom")}
	c := mustNewClient(t, db)
	_, err := c.GetConversationTurnCount(context.Background(), "abc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "GetConversationTurnCount")
}

func TestGetConversationTurnCount_MalformedTurns(t *testing.T) {
	db := &fakeDynamo{
		getOut: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"PK":    &types.AttributeValueMemberS{Value: "CONV#abc"},
				"SK":    &types.AttributeValueMemberS{Value: skMeta},
				"turns": &types.AttributeValueMemberS{Value: "bad"},
			},
		},
	}
	c := mustNewClient(t, db)
	_, err := c.GetConversationTurnCount(context.Background(), "abc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode turns")
}

func TestGetHistory_HappyPath(t *testing.T) {
	db := &fakeDynamo{
		queryOut: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{
				makeItem("CONV#abc", msgSK(time.Now()), "Hello?", "", "complete"),
			},
		},
	}
	c := mustNewClient(t, db)
	msgs, err := c.GetHistory(context.Background(), "abc", 20)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "Hello?", msgs[0].Text)
}

func TestGetHistory_EmptyResult(t *testing.T) {
	db := &fakeDynamo{queryOut: &dynamodb.QueryOutput{}}
	c := mustNewClient(t, db)
	msgs, err := c.GetHistory(context.Background(), "abc", 20)
	require.NoError(t, err)
	require.Empty(t, msgs)
}

func TestGetHistory_QueryError(t *testing.T) {
	db := &fakeDynamo{queryErr: errors.New("ResourceNotFoundException")}
	c := mustNewClient(t, db)
	_, err := c.GetHistory(context.Background(), "abc", 20)
	require.Error(t, err)
	require.Contains(t, err.Error(), "GetHistory")
}

func TestGetHistory_MalformedItem_MissingRole(t *testing.T) {
	item := map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: "CONV#abc"},
		"SK": &types.AttributeValueMemberS{Value: "MSG#ts"},
	}
	db := &fakeDynamo{queryOut: &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{item}}}
	c := mustNewClient(t, db)
	_, err := c.GetHistory(context.Background(), "abc", 20)
	require.Error(t, err)
	require.Contains(t, err.Error(), "text")
}

func TestGetHistory_KeyConditionExpression(t *testing.T) {
	db := &fakeDynamo{queryOut: &dynamodb.QueryOutput{}}
	c := mustNewClient(t, db)
	_, err := c.GetHistory(context.Background(), "abc", 20)
	require.NoError(t, err)
	require.Equal(t, "PK = :pk AND begins_with(SK, :prefix)", *db.lastQueryIn.KeyConditionExpression)
	require.False(t, *db.lastQueryIn.ScanIndexForward)
}

func TestGetHistory_ReordersDescendingResultsToChronological(t *testing.T) {
	db := &fakeDynamo{
		queryOut: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{
				makeItem("CONV#abc", "MSG#2026-02-27T12:00:00Z", "newer", "", "complete"),
				makeItem("CONV#abc", "MSG#2026-02-27T11:00:00Z", "older", "", "complete"),
			},
		},
	}
	c := mustNewClient(t, db)
	msgs, err := c.GetHistory(context.Background(), "abc", 20)
	require.NoError(t, err)
	require.Equal(t, "older", msgs[0].Text)
	require.Equal(t, "newer", msgs[1].Text)
}

func TestWriteMessage_HappyPath(t *testing.T) {
	db := &fakeDynamo{}
	c := mustNewClient(t, db)
	msg := NewMessage("abc", "Who are you?", 4, "complete")
	msg.Answer = "I am your assistant."
	err := c.WriteMessage(context.Background(), msg)
	require.NoError(t, err)
	require.Equal(t, "I am your assistant.", db.lastPutInput.Item["answer"].(*types.AttributeValueMemberS).Value)
	require.Equal(t, "attribute_not_exists(PK) AND attribute_not_exists(SK)", *db.lastPutInput.ConditionExpression)
}

func TestWriteMessage_DynamoError(t *testing.T) {
	db := &fakeDynamo{putErr: errors.New("ProvisionedThroughputExceededException")}
	c := mustNewClient(t, db)
	err := c.WriteMessage(context.Background(), NewMessage("abc", "Who are you?", 4, "complete"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "WriteMessage")
}

func TestWriteMessage_MissingPK(t *testing.T) {
	db := &fakeDynamo{}
	c := mustNewClient(t, db)
	err := c.WriteMessage(context.Background(), domain.Message{SK: "MSG#ts"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestWriteMessage_MissingSK(t *testing.T) {
	db := &fakeDynamo{}
	c := mustNewClient(t, db)
	err := c.WriteMessage(context.Background(), domain.Message{PK: "CONV#abc"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestUpsertMeta_HappyPath(t *testing.T) {
	db := &fakeDynamo{}
	c := mustNewClient(t, db)
	err := c.UpsertMeta(context.Background(), NewConversationMeta("abc", 3))
	require.NoError(t, err)
}

func TestUpsertMeta_DynamoError(t *testing.T) {
	db := &fakeDynamo{putErr: errors.New("internal server error")}
	c := mustNewClient(t, db)
	err := c.UpsertMeta(context.Background(), NewConversationMeta("abc", 1))
	require.Error(t, err)
	require.Contains(t, err.Error(), "UpsertMeta")
}

func TestSaveTurn_HappyPath(t *testing.T) {
	db := &fakeDynamo{}
	c := mustNewClient(t, db)
	msg := NewMessage("abc", "Who are you?", 4, "complete")
	msg.Answer = "I am your assistant."
	meta := NewConversationMeta("abc", 2)

	err := c.SaveTurn(context.Background(), msg, meta)
	require.NoError(t, err)
	require.NotNil(t, db.lastTxInput)
	require.Len(t, db.lastTxInput.TransactItems, 2)
	require.Equal(t, "attribute_not_exists(PK) AND attribute_not_exists(SK)", *db.lastTxInput.TransactItems[0].Put.ConditionExpression)
}

func TestSaveTurn_DynamoError(t *testing.T) {
	db := &fakeDynamo{txErr: errors.New("transaction canceled")}
	c := mustNewClient(t, db)
	err := c.SaveTurn(context.Background(), NewMessage("abc", "Who are you?", 4, "complete"), NewConversationMeta("abc", 2))
	require.Error(t, err)
	require.Contains(t, err.Error(), "SaveTurn")
}

func TestSaveTurn_MissingMessagePK(t *testing.T) {
	db := &fakeDynamo{}
	c := mustNewClient(t, db)
	err := c.SaveTurn(context.Background(), domain.Message{SK: "MSG#ts"}, NewConversationMeta("abc", 1))
	require.Error(t, err)
	require.Contains(t, err.Error(), "message PK")
}

func TestSaveTurn_MissingMetaPK(t *testing.T) {
	db := &fakeDynamo{}
	c := mustNewClient(t, db)
	err := c.SaveTurn(context.Background(), NewMessage("abc", "hi", 0, "complete"), domain.ConversationMeta{SK: skMeta})
	require.Error(t, err)
	require.Contains(t, err.Error(), "meta PK")
}

func TestSaveCompletedTurn_HappyPath(t *testing.T) {
	db := &fakeDynamo{}
	c := mustNewClient(t, db)
	err := c.SaveCompletedTurn(context.Background(), "abc", "Who are you?", "I am your assistant.", 2)
	require.NoError(t, err)
	require.NotNil(t, db.lastTxInput)
	require.Len(t, db.lastTxInput.TransactItems, 2)
}

func TestSaveCompletedTurn_DynamoError(t *testing.T) {
	db := &fakeDynamo{txErr: errors.New("transaction canceled")}
	c := mustNewClient(t, db)
	err := c.SaveCompletedTurn(context.Background(), "abc", "Who are you?", "I am your assistant.", 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SaveCompletedTurn")
}

func TestNewMessage_Fields(t *testing.T) {
	msg := NewMessage("conv-1", "What is Go?", 10, "pending")
	require.Equal(t, "CONV#conv-1", msg.PK)
	require.Contains(t, msg.SK, "MSG#")
	require.Equal(t, "What is Go?", msg.Text)
	require.Equal(t, 10, msg.Tokens)
	require.Greater(t, msg.TTL, int64(0))
}

func TestNewConversationMeta_Fields(t *testing.T) {
	meta := NewConversationMeta("conv-2", 5)
	require.Equal(t, "CONV#conv-2", meta.PK)
	require.Equal(t, skMeta, meta.SK)
	require.Equal(t, 5, meta.Turns)
	require.NotEmpty(t, meta.LastActivity)
}

func TestConvPK(t *testing.T) {
	require.Equal(t, "CONV#my-conv", convPK("my-conv"))
}

func TestMsgSK(t *testing.T) {
	ts := time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC)
	sk := msgSK(ts)
	require.Contains(t, sk, "MSG#")
	require.Contains(t, sk, fmt.Sprintf("%d", ts.Year()))
}

func TestNew_NilAPI(t *testing.T) {
	_, err := New(nil, "test-table")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")
}

func TestNew_EmptyTableName(t *testing.T) {
	_, err := New(&fakeDynamo{}, " ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}
