package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/require"

	"portfolio-agent/internal/usecase"
)

type stubUseCase struct {
	out usecase.AskOutput
	err error
	in  usecase.AskInput
}

func (s *stubUseCase) Ask(_ context.Context, in usecase.AskInput) (usecase.AskOutput, error) {
	s.in = in
	return s.out, s.err
}

func makeEvent(body string) events.APIGatewayProxyRequest {
	return events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodPost,
		Path:       "/ask",
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}
}

func parseBody[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(body), &v))
	return v
}

func TestNewHandler_ValidatesDependency(t *testing.T) {
	_, err := NewHandler(nil)
	require.Error(t, err)
}

func TestHandle_HappyPath(t *testing.T) {
	uc := &stubUseCase{out: usecase.AskOutput{Answer: "hello", ConversationID: "conv-1"}}
	h, err := NewHandler(uc)
	require.NoError(t, err)

	resp, err := h.Handle(context.Background(), makeEvent(`{"question":"What do you do?","conversationId":"conv-1"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, usecase.AskInput{Question: "What do you do?", ConversationID: "conv-1"}, uc.in)

	out := parseBody[askResponse](t, resp.Body)
	require.Equal(t, "hello", out.Answer)
	require.Equal(t, "conv-1", out.ConversationID)
	require.NotEmpty(t, resp.Headers["X-Correlation-Id"])
}

func TestHandle_InvalidBody(t *testing.T) {
	uc := &stubUseCase{}
	h, err := NewHandler(uc)
	require.NoError(t, err)

	resp, err := h.Handle(context.Background(), makeEvent(`not-json`))
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	out := parseBody[errorResponse](t, resp.Body)
	require.Equal(t, string(usecase.ErrorInvalidInput), out.Error)
}

func TestHandle_MapsUseCaseErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "invalid input", err: &usecase.Error{Code: usecase.ErrorInvalidInput, Reason: "empty_question"}, status: http.StatusBadRequest, code: string(usecase.ErrorInvalidInput)},
		{name: "invalid question", err: &usecase.Error{Code: usecase.ErrorInvalidQuestion, Reason: "off_topic"}, status: http.StatusBadRequest, code: string(usecase.ErrorInvalidQuestion)},
		{name: "rate limited", err: &usecase.Error{Code: usecase.ErrorRateLimited, Reason: "openai_rate_limited"}, status: http.StatusTooManyRequests, code: string(usecase.ErrorRateLimited)},
		{name: "upstream", err: &usecase.Error{Code: usecase.ErrorUpstream, Reason: "openai_error"}, status: http.StatusBadGateway, code: string(usecase.ErrorUpstream)},
		{name: "internal", err: &usecase.Error{Code: usecase.ErrorInternal, Reason: "dynamodb_write_error"}, status: http.StatusInternalServerError, code: string(usecase.ErrorInternal)},
		{name: "unexpected", err: errors.New("boom"), status: http.StatusInternalServerError, code: string(usecase.ErrorInternal)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := &stubUseCase{err: tc.err}
			h, err := NewHandler(uc)
			require.NoError(t, err)

			resp, err := h.Handle(context.Background(), makeEvent(`{"question":"What do you do?"}`))
			require.NoError(t, err)
			require.Equal(t, tc.status, resp.StatusCode)

			out := parseBody[errorResponse](t, resp.Body)
			require.Equal(t, tc.code, out.Error)
		})
	}
}

func TestHandle_UsesProvidedCorrelationID_CaseInsensitive(t *testing.T) {
	uc := &stubUseCase{out: usecase.AskOutput{Answer: "ok", ConversationID: "conv-1"}}
	h, err := NewHandler(uc)
	require.NoError(t, err)

	event := makeEvent(`{"question":"What do you do?"}`)
	event.Headers["x-correlation-id"] = "corr-123"
	resp, err := h.Handle(context.Background(), event)
	require.NoError(t, err)
	require.Equal(t, "corr-123", resp.Headers["X-Correlation-Id"])
}
