package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"portfolio-agent/internal/domain"
)

// chatRequest is the minimal request shape for the Chat Completions endpoint.
type chatRequest struct {
	Model          string               `json:"model"`
	Messages       []domain.ChatMessage `json:"messages"`
	Temperature    *float64             `json:"temperature,omitempty"`
	ResponseFormat *responseFormat      `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type       string           `json:"type"`
	JSONSchema jsonSchemaConfig `json:"json_schema"`
}

type jsonSchemaConfig struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

// chatResponse is the minimal response shape returned by the Chat Completions endpoint.
type chatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Choices []struct {
		Index   int                `json:"index"`
		Message domain.ChatMessage `json:"message"`
	} `json:"choices"`
}

// moderationRequest is the request shape for the Moderations endpoint.
type moderationRequest struct {
	Input string `json:"input"`
}

// moderationResponse is the minimal response shape for the Moderations endpoint.
type moderationResponse struct {
	Results []struct {
		Flagged bool `json:"flagged"`
	} `json:"results"`
}

// tokenPayload is the expected JSON shape stored in SSM for the API token.
type tokenPayload struct {
	Token string `json:"token"`
}

type Getter interface {
	GetParameter(ctx context.Context, name string) (string, error)
}

// HTTPStatusError captures non-2xx upstream responses with status-aware context.
type HTTPStatusError struct {
	StatusCode int
	URL        string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("openai: unexpected status %d from %s: %s", e.StatusCode, e.URL, e.Body)
}

func (e *HTTPStatusError) HTTPStatusCode() int {
	return e.StatusCode
}

// Client is a focused OpenAI-compatible client for chat completions.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	getter      Getter
	paramPrefix string

	keyOnce sync.Once
	apiKey  string
	keyErr  error
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimSpace(baseURL)
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// NewClient creates a new Client backed by the given paramstore.Getter for
// API key retrieval. The key is fetched from SSM on the first call to Chat or
// Moderate and reused for the lifetime of the process.
func NewClient(ps Getter, paramPrefix string, opts ...Option) (*Client, error) {
	if ps == nil {
		return nil, errors.New("openai: paramstore getter must not be nil")
	}
	paramPrefix = strings.TrimRight(strings.TrimSpace(paramPrefix), "/")
	if paramPrefix == "" {
		return nil, errors.New("openai: parameter prefix must not be empty")
	}
	c := &Client{
		baseURL:     "https://api.openai.com/v1",
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		getter:      ps,
		paramPrefix: paramPrefix,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// resolveAPIKey fetches the API apiKey from SSM on the first call and returns the
// cached result on every subsequent call within the same process lifetime.
func (c *Client) resolveAPIKey(ctx context.Context) (string, error) {
	c.keyOnce.Do(func() {
		c.apiKey, c.keyErr = fetchAPIKeyFromParamStore(ctx, c.getter, c.tokenParameterName())
	})
	return c.apiKey, c.keyErr
}

func (c *Client) tokenParameterName() string {
	return c.paramPrefix + "/open-ai-token"
}

// httpClient returns the configured HTTP client, or a default with a 10s timeout
// if none was set (e.g. in tests that nil out the field).
func (c *Client) resolvedHTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func chatURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func (c *Client) Chat(ctx context.Context, model string, messages []domain.ChatMessage) (string, error) {
	if model == "" {
		return "", errors.New("openai: model must not be empty")
	}

	apiKey, err := c.resolveAPIKey(ctx)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(chatRequest{
		Model:          model,
		Messages:       messages,
		ResponseFormat: scopedAnswerResponseFormat(),
	})
	if err != nil {
		return "", fmt.Errorf("openai: marshal request: %w", err)
	}

	url := chatURL(c.baseURL)

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if reqErr != nil {
		return "", fmt.Errorf("openai: create request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	raw, err := c.doJSONRequest(req, url)
	if err != nil {
		return "", fmt.Errorf("openai: request failed: %w", err)
	}

	var payload chatResponse
	if decErr := json.Unmarshal(raw, &payload); decErr != nil {
		return "", fmt.Errorf("openai: decode response: %w", decErr)
	}
	if len(payload.Choices) == 0 {
		return "", errors.New("openai: no choices in response")
	}
	result := payload.Choices[0].Message.Content

	return result, nil
}

func scopedAnswerResponseFormat() *responseFormat {
	return &responseFormat{
		Type: "json_schema",
		JSONSchema: jsonSchemaConfig{
			Name:   "scoped_answer",
			Strict: true,
			Schema: json.RawMessage(`{
				"type":"object",
				"additionalProperties":false,
				"properties":{
					"in_scope":{"type":"boolean"},
					"answer":{"type":"string"}
				},
				"required":["in_scope","answer"]
			}`),
		},
	}
}

func moderationURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/moderations"
	}
	return base + "/v1/moderations"
}

// Moderate calls the OpenAI Moderations API and returns true if the input is flagged.
func (c *Client) Moderate(ctx context.Context, input string) (bool, error) {
	apiKey, err := c.resolveAPIKey(ctx)
	if err != nil {
		return false, err
	}

	body, err := json.Marshal(moderationRequest{Input: input})
	if err != nil {
		return false, fmt.Errorf("openai: marshal moderation request: %w", err)
	}

	url := moderationURL(c.baseURL)

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if reqErr != nil {
		return false, fmt.Errorf("openai: create moderation request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	raw, err := c.doJSONRequest(req, url)
	if err != nil {
		return false, fmt.Errorf("openai: moderation request failed: %w", err)
	}

	var payload moderationResponse
	if decErr := json.Unmarshal(raw, &payload); decErr != nil {
		return false, fmt.Errorf("openai: decode moderation response: %w", decErr)
	}
	if len(payload.Results) == 0 {
		return false, errors.New("openai: no results in moderation response")
	}
	flagged := payload.Results[0].Flagged

	return flagged, nil
}

func (c *Client) doJSONRequest(req *http.Request, url string) ([]byte, error) {
	res, doErr := c.resolvedHTTPClient().Do(req)
	if doErr != nil {
		return nil, doErr
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, &HTTPStatusError{
			StatusCode: res.StatusCode,
			URL:        url,
			Body:       string(buf),
		}
	}

	buf, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return buf, nil
}

func fetchAPIKeyFromParamStore(ctx context.Context, getter Getter, name string) (string, error) {
	if getter == nil {
		return "", errors.New("openai: paramstore getter is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("openai: token parameter name is empty")
	}

	raw, err := getter.GetParameter(ctx, name)
	if err != nil {
		return "", fmt.Errorf("openai: fetch token from paramstore: %w", err)
	}
	var tp tokenPayload
	if err := json.Unmarshal([]byte(raw), &tp); err != nil {
		return "", fmt.Errorf("openai: unmarshal paramstore token value as JSON: %w", err)
	}
	if tp.Token == "" {
		return "", fmt.Errorf("openai: API token is empty")
	}
	return tp.Token, nil
}
