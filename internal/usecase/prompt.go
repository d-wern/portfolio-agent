package usecase

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"portfolio-agent/internal/domain"
)

type scopedAnswerResponse struct {
	InScope bool   `json:"in_scope"`
	Answer  string `json:"answer"`
}

type promptContext struct {
	pinnedPrompt string
	resume       string
	interests    string
}

func buildPromptMessages(ctx promptContext, question string, history []domain.Message) []domain.ChatMessage {
	messages := []domain.ChatMessage{
		{Role: "system", Content: buildPolicyPrompt()},
		{Role: "system", Content: buildProfileContextPrompt(ctx)},
	}

	for _, m := range history {
		messages = append(messages, historyToPromptMessages(m)...)
	}

	messages = append(messages, domain.ChatMessage{
		Role:    "user",
		Content: question,
	})
	return messages
}

func buildPolicyPrompt() string {
	return strings.Join([]string{
		"Role:",
		"You are answering as the portfolio owner in first person.",
		"",
		"Task:",
		"Determine whether the current question is relevant to recruiting for a professional role.",
		"If relevant, answer using only the approved sources.",
		"If not relevant, return out of scope.",
		"",
		"Approved Sources:",
		"- Resume content provided in this request",
		"- Interests provided in this request",
		"- Completed prior conversation turns in this request",
		"",
		"Behavior Rules:",
		behaviorRules(),
		"",
		"Output Contract:",
		outputContract(),
	}, "\n")
}

func buildProfileContextPrompt(ctx promptContext) string {
	return fmt.Sprintf(
		"%s\n\nPortfolio Context:\n\nResume:\n%s\n\nInterests:\n%s",
		strings.TrimSpace(ctx.pinnedPrompt),
		normalizePromptInput(ctx.resume),
		normalizePromptInput(ctx.interests),
	)
}

func historyToPromptMessages(m domain.Message) []domain.ChatMessage {
	if m.Status != statusComplete {
		return nil
	}
	question := strings.TrimSpace(m.Text)
	answer := strings.TrimSpace(m.Answer)
	if question == "" || answer == "" {
		return nil
	}
	return []domain.ChatMessage{
		{Role: "user", Content: question},
		{Role: "assistant", Content: answer},
	}
}

func behaviorRules() string {
	return strings.Join([]string{
		"1) Answer only the current user question in this request.",
		"2) Use first-person voice as the portfolio owner.",
		"3) Keep responses professional and concise.",
		"4) Use only resume, interests, and completed conversation history as sources.",
		"5) Treat questions unrelated to recruiting for a professional role as off-topic.",
		"6) If required information is unavailable, respond exactly: \"I don't have that information.\"",
	}, "\n")
}

func outputContract() string {
	return "Return JSON only with keys in_scope (boolean) and answer (string). " +
		"If out of scope, return in_scope=false and answer=\"\". " +
		"If in scope, return in_scope=true and provide the final user-facing answer in answer."
}

func normalizePromptInput(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func parseScopedAnswer(raw string) (scopedAnswerResponse, error) {
	var out scopedAnswerResponse
	dec := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return scopedAnswerResponse{}, fmt.Errorf("usecase: decode scoped answer: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return scopedAnswerResponse{}, errors.New("usecase: decode scoped answer: multiple JSON values")
		}
		return scopedAnswerResponse{}, fmt.Errorf("usecase: decode scoped answer trailing data: %w", err)
	}
	if out.InScope && strings.TrimSpace(out.Answer) == "" {
		return scopedAnswerResponse{}, errors.New("usecase: scoped answer missing answer for in-scope question")
	}
	return out, nil
}
