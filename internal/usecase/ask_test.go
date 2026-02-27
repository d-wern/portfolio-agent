package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"portfolio-agent/internal/domain"
	"portfolio-agent/internal/integrations/openai"
)

type mockParams struct {
	vals map[string]string
	err  error
}

func (m *mockParams) GetParameter(_ context.Context, name string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	v, ok := m.vals[name]
	if !ok {
		return "", fmt.Errorf("param not found: %s", name)
	}
	return v, nil
}

type transientParams struct {
	*mockParams
	failOnce bool
}

func (p *transientParams) GetParameter(ctx context.Context, name string) (string, error) {
	if p.failOnce {
		p.failOnce = false
		return "", errors.New("temporary ssm failure")
	}
	return p.mockParams.GetParameter(ctx, name)
}

type chatResponse struct {
	answer string
	err    error
}

type mockLLM struct {
	responses []chatResponse
	callCount int
	flagged   bool
	err       error
}

func (m *mockLLM) Chat(_ context.Context, _ string, _ []domain.ChatMessage) (string, error) {
	if len(m.responses) == 0 {
		return "", errors.New("no llm response configured")
	}
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx].answer, m.responses[idx].err
}

func (m *mockLLM) Moderate(_ context.Context, _ string) (bool, error) {
	return m.flagged, m.err
}

type mockState struct {
	history              []domain.Message
	turnCount            int
	historyErr           error
	turnCountErr         error
	saveErr              error
	savedConversationID  string
	savedQuestion        string
	savedAnswer          string
	savedTurns           int
	saveCompletedInvoked bool
}

func (m *mockState) GetConversationTurnCount(_ context.Context, _ string) (int, error) {
	return m.turnCount, m.turnCountErr
}

func (m *mockState) GetHistory(_ context.Context, _ string, _ int) ([]domain.Message, error) {
	return m.history, m.historyErr
}

func (m *mockState) SaveCompletedTurn(_ context.Context, conversationID, question, answer string, turns int) error {
	m.savedConversationID = conversationID
	m.savedQuestion = question
	m.savedAnswer = answer
	m.savedTurns = turns
	m.saveCompletedInvoked = true
	return m.saveErr
}

type capturingLLM struct {
	answer    string
	err       error
	captured  *[]domain.ChatMessage
	callCount int
}

func (c *capturingLLM) Chat(_ context.Context, _ string, msgs []domain.ChatMessage) (string, error) {
	c.callCount++
	*c.captured = msgs
	return c.answer, c.err
}

func (c *capturingLLM) Moderate(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func defaultParams() *mockParams {
	return &mockParams{
		vals: map[string]string{
			"/prefix/resume":              "Software Engineer with 5 years experience.",
			"/prefix/interests":           "Go, distributed systems, open source.",
			"/prefix/pinned_prompt":       "You are a helpful assistant.",
			"/prefix/config/openai_model": "gpt-4o-mini",
		},
	}
}

func scopedResponse(inScope bool, answer string) string {
	return fmt.Sprintf(`{"in_scope":%t,"answer":%q}`, inScope, answer)
}

func pass() *mockLLM { return &mockLLM{flagged: false} }
func flag() *mockLLM { return &mockLLM{flagged: true} }

func newTestService(t *testing.T, p ParamGetter, llm LLMClient, s StateReadWriter) *AskService {
	t.Helper()
	svc, err := NewAskService(p, llm, s, "/prefix", 20, 300)
	require.NoError(t, err)
	return svc
}

func expectAskError(t *testing.T, err error, code ErrorCode, reason string) {
	t.Helper()
	var usecaseErr *Error
	require.ErrorAs(t, err, &usecaseErr)
	require.Equal(t, code, usecaseErr.Code)
	require.Equal(t, reason, usecaseErr.Reason)
}

func TestNewAskService_ValidatesDependencies(t *testing.T) {
	_, err := NewAskService(nil, pass(), &mockState{}, "/prefix", 20, 300)
	require.Error(t, err)

	_, err = NewAskService(defaultParams(), nil, &mockState{}, "/prefix", 20, 300)
	require.Error(t, err)

	_, err = NewAskService(defaultParams(), pass(), nil, "/prefix", 20, 300)
	require.Error(t, err)

	_, err = NewAskService(defaultParams(), pass(), &mockState{}, " ", 20, 300)
	require.Error(t, err)
}

func TestAsk_HappyPath(t *testing.T) {
	state := &mockState{}
	llm := &mockLLM{responses: []chatResponse{{answer: scopedResponse(true, "I am a software engineer.")}}}
	svc := newTestService(t, defaultParams(), llm, state)

	out, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?", ConversationID: "conv-1"})
	require.NoError(t, err)
	require.Equal(t, "I am a software engineer.", out.Answer)
	require.Equal(t, "conv-1", out.ConversationID)
	require.True(t, state.saveCompletedInvoked)
	require.Equal(t, "conv-1", state.savedConversationID)
	require.Equal(t, "What do you do?", state.savedQuestion)
	require.Equal(t, "I am a software engineer.", state.savedAnswer)
	require.Equal(t, 1, state.savedTurns)
}

func TestAsk_MissingConversationID_GeneratesID(t *testing.T) {
	llm := &mockLLM{responses: []chatResponse{{answer: scopedResponse(true, "Sure.")}}}
	svc := newTestService(t, defaultParams(), llm, &mockState{})

	out, err := svc.Ask(context.Background(), AskInput{Question: "What technologies do you use?"})
	require.NoError(t, err)
	require.NotEmpty(t, out.ConversationID)
}

func TestAsk_ValidationErrors(t *testing.T) {
	svc := newTestService(t, defaultParams(), pass(), &mockState{})

	_, err := svc.Ask(context.Background(), AskInput{Question: ""})
	expectAskError(t, err, ErrorInvalidInput, "empty_question")

	_, err = svc.Ask(context.Background(), AskInput{Question: strings.Repeat("a", 301)})
	expectAskError(t, err, ErrorInvalidInput, "question_too_long")
}

func TestAsk_RelevanceOffTopic(t *testing.T) {
	svc := newTestService(t, defaultParams(), &mockLLM{responses: []chatResponse{{answer: scopedResponse(false, "")}}}, &mockState{})
	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you think about politics?"})
	expectAskError(t, err, ErrorInvalidQuestion, "relevance_off_topic")
}

func TestAsk_MalformedScopedResponse(t *testing.T) {
	svc := newTestService(t, defaultParams(), &mockLLM{responses: []chatResponse{{answer: "not-json"}}}, &mockState{})
	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorUpstream, "openai_malformed_response")
}

func TestAsk_ModerationErrors(t *testing.T) {
	svc := newTestService(t, defaultParams(), flag(), &mockState{})
	_, err := svc.Ask(context.Background(), AskInput{Question: "unsafe"})
	expectAskError(t, err, ErrorInvalidQuestion, "moderation_flagged")

	svc = newTestService(t, defaultParams(), &mockLLM{err: &openai.HTTPStatusError{StatusCode: http.StatusInternalServerError}}, &mockState{})
	_, err = svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorUpstream, "moderation_error")

	svc = newTestService(t, defaultParams(), &mockLLM{err: &openai.HTTPStatusError{StatusCode: http.StatusTooManyRequests}}, &mockState{})
	_, err = svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorRateLimited, "moderation_rate_limited")
}

func TestAsk_SSMLoadErrors(t *testing.T) {
	svc := newTestService(t, &mockParams{err: errors.New("ssm unavailable")}, pass(), &mockState{})
	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorInternal, "ssm_load_error")

	p := defaultParams()
	delete(p.vals, "/prefix/pinned_prompt")
	svc = newTestService(t, p, pass(), &mockState{})
	_, err = svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorInternal, "ssm_load_error")
}

func TestAsk_SSMLoadError_IsRetriedOnNextRequest(t *testing.T) {
	p := &transientParams{mockParams: defaultParams(), failOnce: true}
	llm := &mockLLM{responses: []chatResponse{{answer: scopedResponse(true, "ok")}}}
	svc := newTestService(t, p, llm, &mockState{})

	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorInternal, "ssm_load_error")

	out, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	require.NoError(t, err)
	require.Equal(t, "ok", out.Answer)
}

func TestAsk_StateErrors(t *testing.T) {
	svc := newTestService(t, defaultParams(), &mockLLM{responses: []chatResponse{{answer: scopedResponse(true, "ok")}}}, &mockState{historyErr: errors.New("dynamodb down")})
	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorInternal, "dynamodb_history_error")

	svc = newTestService(t, defaultParams(), &mockLLM{responses: []chatResponse{{answer: scopedResponse(true, "ok")}}}, &mockState{turnCountErr: errors.New("meta read failed")})
	_, err = svc.Ask(context.Background(), AskInput{Question: "What do you do?", ConversationID: "conv-1"})
	expectAskError(t, err, ErrorInternal, "dynamodb_turn_count_error")

	svc = newTestService(t, defaultParams(), &mockLLM{responses: []chatResponse{{answer: scopedResponse(true, "ok")}}}, &mockState{saveErr: errors.New("write failed")})
	_, err = svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorInternal, "dynamodb_write_error")
}

func TestAsk_ConversationTurnLimit(t *testing.T) {
	state := &mockState{turnCount: 10}
	llm := &mockLLM{responses: []chatResponse{{answer: scopedResponse(true, "ok")}}}
	svc := newTestService(t, defaultParams(), llm, state)

	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?", ConversationID: "conv-1"})
	expectAskError(t, err, ErrorInvalidInput, "conversation_turn_limit")
	require.Zero(t, llm.callCount)
	require.False(t, state.saveCompletedInvoked)
}

func TestAsk_SaveTurn_UsesPersistedTurnCount(t *testing.T) {
	state := &mockState{
		turnCount: 9,
		history:   make([]domain.Message, 20),
	}
	llm := &mockLLM{responses: []chatResponse{{answer: scopedResponse(true, "great answer")}}}
	svc := newTestService(t, defaultParams(), llm, state)

	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?", ConversationID: "conv-1"})
	require.NoError(t, err)
	require.True(t, state.saveCompletedInvoked)
	require.Equal(t, 10, state.savedTurns)
}

func TestAsk_OpenAIErrors(t *testing.T) {
	svc := newTestService(t, defaultParams(), &mockLLM{responses: []chatResponse{{err: &openai.HTTPStatusError{StatusCode: http.StatusTooManyRequests}}}}, &mockState{})
	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorRateLimited, "openai_rate_limited")

	svc = newTestService(t, defaultParams(), &mockLLM{responses: []chatResponse{{err: &openai.HTTPStatusError{StatusCode: http.StatusInternalServerError}}}}, &mockState{})
	_, err = svc.Ask(context.Background(), AskInput{Question: "What do you do?"})
	expectAskError(t, err, ErrorUpstream, "openai_error")
}

func TestAsk_BuildMessages_UsesOnlyCompletedTurns(t *testing.T) {
	history := []domain.Message{
		{Text: "What is your background?", Answer: "I am a software engineer."},
		{Text: "This question should not be replayed"},
		{Text: "This pending assistant text should not be replayed"},
	}
	var captured []domain.ChatMessage
	llm := &capturingLLM{answer: scopedResponse(true, "ok"), captured: &captured}
	svc := newTestService(t, defaultParams(), llm, &mockState{history: history})

	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do now?"})
	require.NoError(t, err)
	require.Len(t, captured, 5)
	require.Equal(t, "What is your background?", captured[2].Content)
	require.Equal(t, "I am a software engineer.", captured[3].Content)
	require.Equal(t, "What do you do now?", captured[4].Content)
}

func TestAsk_BuildMessages_IncludesAllCompletedTurnsInWindow(t *testing.T) {
	history := []domain.Message{
		{Text: "What is your background?", Answer: "I am a software engineer."},
		{Text: "What do you enjoy building?", Answer: "I enjoy distributed systems."},
	}
	var captured []domain.ChatMessage
	llm := &capturingLLM{answer: scopedResponse(true, "ok"), captured: &captured}
	svc := newTestService(t, defaultParams(), llm, &mockState{history: history})

	_, err := svc.Ask(context.Background(), AskInput{Question: "What do you do now?"})
	require.NoError(t, err)
	require.Len(t, captured, 7)
	require.Equal(t, "What is your background?", captured[2].Content)
	require.Equal(t, "I am a software engineer.", captured[3].Content)
	require.Equal(t, "What do you enjoy building?", captured[4].Content)
	require.Equal(t, "I enjoy distributed systems.", captured[5].Content)
}

func TestBuildProfileContextPrompt_IncludesProfileContext(t *testing.T) {
	content := buildProfileContextPrompt(promptContext{
		pinnedPrompt: "Pinned prompt",
		resume:       "Resume text",
		interests:    "Interests text",
	})
	require.Contains(t, content, "Pinned prompt")
	require.Contains(t, content, "Portfolio Context:")
	require.Contains(t, content, "Resume:")
	require.Contains(t, content, "Interests:")
}

func TestBuildPolicyPrompt_IncludesRules(t *testing.T) {
	content := buildPolicyPrompt()
	require.Contains(t, content, "Role:")
	require.Contains(t, content, "Approved Sources:")
	require.Contains(t, content, "Behavior Rules:")
	require.Contains(t, content, "Answer only the current user question")
	require.Contains(t, content, "Output Contract:")
	require.Contains(t, content, "Return JSON only with keys in_scope")
}

func TestParseScopedAnswer(t *testing.T) {
	out, err := parseScopedAnswer(`{"in_scope":true,"answer":"hello"}`)
	require.NoError(t, err)
	require.True(t, out.InScope)
	require.Equal(t, "hello", out.Answer)

	_, err = parseScopedAnswer(`{"in_scope":true,"answer":""}`)
	require.Error(t, err)

	_, err = parseScopedAnswer(`not-json`)
	require.Error(t, err)

	_, err = parseScopedAnswer(`{"in_scope":true,"answer":"wrapped","extra":true}`)
	require.Error(t, err)
}
