package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"

	"portfolio-agent/internal/usecase"
)

type AskUseCase interface {
	Ask(ctx context.Context, in usecase.AskInput) (usecase.AskOutput, error)
}

type Handler struct {
	ask AskUseCase
}

type askRequest struct {
	Question       string `json:"question"`
	ConversationID string `json:"conversationId"`
}

type askResponse struct {
	Answer         string `json:"answer"`
	ConversationID string `json:"conversationId"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(askUseCase AskUseCase) (*Handler, error) {
	if askUseCase == nil {
		return nil, errors.New("handler: ask use case must not be nil")
	}
	return &Handler{ask: askUseCase}, nil
}

func (h *Handler) Handle(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	correlationID := headerValue(event.Headers, "X-Correlation-Id")
	if correlationID == "" {
		correlationID = uuid.NewString()
	}
	requestID := event.RequestContext.RequestID

	log := slog.With("correlation_id", correlationID, "request_id", requestID)
	log.InfoContext(ctx, "ask.request.count", "method", event.HTTPMethod, "path", event.Path)

	start := time.Now()

	var req askRequest
	if err := json.Unmarshal([]byte(event.Body), &req); err != nil {
		return rejectResponse(ctx, log, correlationID, http.StatusBadRequest, string(usecase.ErrorInvalidInput), "invalid_body", start), nil
	}

	out, err := h.ask.Ask(ctx, usecase.AskInput{
		Question:       req.Question,
		ConversationID: req.ConversationID,
	})
	if err != nil {
		return rejectForUseCaseError(ctx, log, correlationID, err, start), nil
	}

	latencyMs := time.Since(start).Milliseconds()
	log.InfoContext(ctx, "ask.invoked", "event", "ask.invoked", "conversation_id", out.ConversationID, "latency_ms", latencyMs)
	log.InfoContext(ctx, "ask.request.latency", "latency_ms", latencyMs)

	return jsonResponse(http.StatusOK, askResponse{
		Answer:         out.Answer,
		ConversationID: out.ConversationID,
	}, correlationID), nil
}

func rejectForUseCaseError(ctx context.Context, log *slog.Logger, correlationID string, err error, start time.Time) events.APIGatewayProxyResponse {
	var askErr *usecase.Error
	if errors.As(err, &askErr) {
		switch askErr.Code {
		case usecase.ErrorInvalidInput:
			return rejectResponse(ctx, log, correlationID, http.StatusBadRequest, string(askErr.Code), askErr.Reason, start)
		case usecase.ErrorInvalidQuestion:
			return rejectResponse(ctx, log, correlationID, http.StatusBadRequest, string(askErr.Code), askErr.Reason, start)
		case usecase.ErrorRateLimited:
			return rejectResponse(ctx, log, correlationID, http.StatusTooManyRequests, string(askErr.Code), askErr.Reason, start)
		case usecase.ErrorUpstream:
			return rejectResponse(ctx, log, correlationID, http.StatusBadGateway, string(askErr.Code), askErr.Reason, start)
		default:
			return rejectResponse(ctx, log, correlationID, http.StatusInternalServerError, string(usecase.ErrorInternal), askErr.Reason, start)
		}
	}
	return rejectResponse(ctx, log, correlationID, http.StatusInternalServerError, string(usecase.ErrorInternal), "unexpected_error", start)
}

func rejectResponse(ctx context.Context, log *slog.Logger, correlationID string, statusCode int, errorCode, reason string, start time.Time) events.APIGatewayProxyResponse {
	log.WarnContext(ctx, "ask.rejected", "event", "ask.rejected", "reason", reason, "http_status", statusCode, "latency_ms", time.Since(start).Milliseconds())
	log.InfoContext(ctx, "ask.request.rejected", "http_status", statusCode, "reason", reason)
	return jsonResponse(statusCode, errorResponse{Error: errorCode}, correlationID)
}

func headerValue(headers map[string]string, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func jsonResponse(statusCode int, v any, correlationID string) events.APIGatewayProxyResponse {
	body, err := json.Marshal(v)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Headers:    baseHeaders(correlationID),
			Body:       `{"error":"INTERNAL_ERROR"}`,
		}
	}
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers:    baseHeaders(correlationID),
		Body:       string(body),
	}
}

func baseHeaders(correlationID string) map[string]string {
	return map[string]string{
		"Content-Type":                  "application/json",
		"X-Correlation-Id":              correlationID,
		"Access-Control-Allow-Origin":   "*",
		"Access-Control-Allow-Methods":  "OPTIONS,POST",
		"Access-Control-Allow-Headers":  "Content-Type,X-Correlation-Id",
		"Access-Control-Expose-Headers": "X-Correlation-Id",
	}
}
