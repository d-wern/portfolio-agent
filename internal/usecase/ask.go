package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"portfolio-agent/internal/domain"
)

const (
	defaultMaxContext    = 20
	defaultMaxQuestion   = 300
	maxConversationTurns = 10
	statusComplete       = "complete"
)

type ParamGetter interface {
	GetParameter(ctx context.Context, name string) (string, error)
}

type LLMClient interface {
	Chat(ctx context.Context, model string, messages []domain.ChatMessage) (string, error)
	Moderate(ctx context.Context, input string) (bool, error)
}

type StateReadWriter interface {
	GetConversationTurnCount(ctx context.Context, conversationID string) (int, error)
	GetHistory(ctx context.Context, conversationID string, limit int) ([]domain.Message, error)
	SaveCompletedTurn(ctx context.Context, conversationID, question, answer string, turns int) error
}

type httpStatusCoder interface {
	HTTPStatusCode() int
}

type AskService struct {
	params          ParamGetter
	llm             LLMClient
	state           StateReadWriter
	paramPrefix     string
	maxContextItems int
	maxQuestionLen  int

	cacheMu      sync.RWMutex
	cacheLoaded  bool
	resume       string
	interests    string
	pinnedPrompt string
	openaiModel  string
}

type AskInput struct {
	Question       string
	ConversationID string
}

type AskOutput struct {
	Answer         string
	ConversationID string
}

func NewAskService(p ParamGetter, llm LLMClient, s StateReadWriter, paramPrefix string, maxContextItems, maxQuestionLen int) (*AskService, error) {
	if p == nil {
		return nil, errors.New("usecase: param getter must not be nil")
	}
	if llm == nil {
		return nil, errors.New("usecase: llm client must not be nil")
	}
	if s == nil {
		return nil, errors.New("usecase: state store must not be nil")
	}
	paramPrefix = strings.TrimRight(strings.TrimSpace(paramPrefix), "/")
	if paramPrefix == "" {
		return nil, errors.New("usecase: parameter prefix must not be empty")
	}
	if maxContextItems <= 0 {
		maxContextItems = defaultMaxContext
	}
	if maxQuestionLen <= 0 {
		maxQuestionLen = defaultMaxQuestion
	}
	return &AskService{
		params:          p,
		llm:             llm,
		state:           s,
		paramPrefix:     paramPrefix,
		maxContextItems: maxContextItems,
		maxQuestionLen:  maxQuestionLen,
	}, nil
}

func (s *AskService) Ask(ctx context.Context, in AskInput) (AskOutput, error) {
	question := strings.TrimSpace(in.Question)
	if question == "" {
		return AskOutput{}, newError(ErrorInvalidInput, "empty_question", nil)
	}
	if len(question) > s.maxQuestionLen {
		return AskOutput{}, newError(ErrorInvalidInput, "question_too_long", nil)
	}
	if err := s.ensureConfig(ctx); err != nil {
		return AskOutput{}, newError(ErrorInternal, "ssm_load_error", err)
	}
	convID := strings.TrimSpace(in.ConversationID)
	if convID == "" {
		convID = newUUID()
	}

	existingTurns := 0
	if strings.TrimSpace(in.ConversationID) != "" {
		turnCount, err := s.state.GetConversationTurnCount(ctx, convID)
		if err != nil {
			return AskOutput{}, newError(ErrorInternal, "dynamodb_turn_count_error", err)
		}
		existingTurns = turnCount
		if existingTurns >= maxConversationTurns {
			return AskOutput{}, newError(ErrorInvalidInput, "conversation_turn_limit", nil)
		}
	}

	flagged, err := s.llm.Moderate(ctx, question)
	if err != nil {
		if status, ok := upstreamStatusCode(err); ok && status == 429 {
			return AskOutput{}, newError(ErrorRateLimited, "moderation_rate_limited", err)
		}
		return AskOutput{}, newError(ErrorUpstream, "moderation_error", err)
	}
	if flagged {
		return AskOutput{}, newError(ErrorInvalidQuestion, "moderation_flagged", nil)
	}

	history, err := s.state.GetHistory(ctx, convID, s.maxContextItems)
	if err != nil {
		return AskOutput{}, newError(ErrorInternal, "dynamodb_history_error", err)
	}

	raw, err := s.llm.Chat(ctx, s.openaiModel, buildPromptMessages(
		promptContext{
			pinnedPrompt: s.pinnedPrompt,
			resume:       s.resume,
			interests:    s.interests,
		},
		question,
		history,
	))
	if err != nil {
		if status, ok := upstreamStatusCode(err); ok && status == 429 {
			return AskOutput{}, newError(ErrorRateLimited, "openai_rate_limited", err)
		}
		return AskOutput{}, newError(ErrorUpstream, "openai_error", err)
	}

	decision, err := parseScopedAnswer(raw)
	if err != nil {
		return AskOutput{}, newError(ErrorUpstream, "openai_malformed_response", err)
	}
	if !decision.InScope {
		return AskOutput{}, newError(ErrorInvalidQuestion, "relevance_off_topic", nil)
	}

	if err := s.state.SaveCompletedTurn(ctx, convID, question, decision.Answer, existingTurns+1); err != nil {
		return AskOutput{}, newError(ErrorInternal, "dynamodb_write_error", err)
	}

	return AskOutput{
		Answer:         decision.Answer,
		ConversationID: convID,
	}, nil
}

func (s *AskService) ensureConfig(ctx context.Context) error {
	s.cacheMu.RLock()
	if s.cacheLoaded {
		s.cacheMu.RUnlock()
		return nil
	}
	s.cacheMu.RUnlock()

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cacheLoaded {
		return nil
	}

	resume, interests, pinnedPrompt, openaiModel, err := s.loadSSMParams(ctx)
	if err != nil {
		return err
	}

	s.resume = resume
	s.interests = interests
	s.pinnedPrompt = pinnedPrompt
	s.openaiModel = openaiModel
	s.cacheLoaded = true
	return nil
}

func (s *AskService) loadSSMParams(ctx context.Context) (resume, interests, pinnedPrompt, openaiModel string, err error) {
	prefix := strings.TrimRight(s.paramPrefix, "/")

	resume, err = s.params.GetParameter(ctx, prefix+"/resume")
	if err != nil {
		return "", "", "", "", fmt.Errorf("usecase: load resume: %w", err)
	}
	interests, err = s.params.GetParameter(ctx, prefix+"/interests")
	if err != nil {
		return "", "", "", "", fmt.Errorf("usecase: load interests: %w", err)
	}
	pinnedPrompt, err = s.params.GetParameter(ctx, prefix+"/pinned_prompt")
	if err != nil {
		return "", "", "", "", fmt.Errorf("usecase: load pinned prompt: %w", err)
	}
	openaiModel, err = s.params.GetParameter(ctx, prefix+"/config/openai_model")
	if err != nil {
		return "", "", "", "", fmt.Errorf("usecase: load openai model: %w", err)
	}
	return resume, interests, pinnedPrompt, openaiModel, nil
}

func upstreamStatusCode(err error) (int, bool) {
	var statusErr httpStatusCoder
	if !errors.As(err, &statusErr) {
		return 0, false
	}
	return statusErr.HTTPStatusCode(), true
}

var newUUID = func() string {
	return uuid.NewString()
}
