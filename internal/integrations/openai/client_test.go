package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"portfolio-agent/internal/domain"
)

// ---------------------------------------------------------------------------
// chatURL helper
// ---------------------------------------------------------------------------

func TestChatURL(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"http://localhost:8080", "http://localhost:8080/v1/chat/completions"},
		{"", "https://api.openai.com/v1/chat/completions"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, chatURL(tc.base), "base=%q", tc.base)
	}
}

// ---------------------------------------------------------------------------
// NewClient
// ---------------------------------------------------------------------------

func TestNewClient_NilGetter(t *testing.T) {
	_, err := NewClient(nil, "/portfolio-agent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestNewClient_Valid(t *testing.T) {
	g := &fakeGetter{}
	c, err := NewClient(g, "/portfolio-agent")
	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1", c.baseURL)
	require.NotNil(t, c.getter)
}

// ---------------------------------------------------------------------------
// resolveAPIKey — SSM caching behaviour
// ---------------------------------------------------------------------------

func TestResolveAPIKey_FetchedOnFirstCall(t *testing.T) {
	calls := 0
	g := &fakeGetter{val: `{"token":"sk-from-ssm"}`}
	g.onCall = func() { calls++ }
	c, err := NewClient(g, "/portfolio-agent")
	require.NoError(t, err)

	key, err := c.resolveAPIKey(context.Background())
	require.NoError(t, err)
	require.Equal(t, "sk-from-ssm", key)
	require.Equal(t, 1, calls)

	// subsequent calls must never hit SSM again
	_, _ = c.resolveAPIKey(context.Background())
	_, _ = c.resolveAPIKey(context.Background())
	require.Equal(t, 1, calls, "SSM must only be called once per process lifetime")
}

// ---------------------------------------------------------------------------
// fetchAPIKeyFromParamStore
// ---------------------------------------------------------------------------

// fakeGetter is a minimal paramstore.Getter stub for use within this package.
type fakeGetter struct {
	val    string
	err    error
	onCall func() // optional; called on each GetParameter invocation
}

func (f *fakeGetter) GetParameter(_ context.Context, _ string) (string, error) {
	if f.onCall != nil {
		f.onCall()
	}
	return f.val, f.err
}

func TestFetchAPIKey_JSONToken(t *testing.T) {
	g := &fakeGetter{val: `{"token":"sk-from-json"}`}
	key, err := fetchAPIKeyFromParamStore(context.Background(), g, "/portfolio-agent/open-ai-token")
	require.NoError(t, err)
	require.Equal(t, "sk-from-json", key)
}

func TestFetchAPIKey_JSONMissingTokenField(t *testing.T) {
	g := &fakeGetter{val: `{"other":"value"}`}
	_, err := fetchAPIKeyFromParamStore(context.Background(), g, "/portfolio-agent/open-ai-token")
	require.Error(t, err)
	require.Contains(t, err.Error(), "API token is empty")
}

func TestFetchAPIKey_MalformedJSON(t *testing.T) {
	g := &fakeGetter{val: `{"broken`}
	_, err := fetchAPIKeyFromParamStore(context.Background(), g, "/portfolio-agent/open-ai-token")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

func TestFetchAPIKey_GetterError(t *testing.T) {
	g := &fakeGetter{err: errors.New("ssm unavailable")}
	_, err := fetchAPIKeyFromParamStore(context.Background(), g, "/portfolio-agent/open-ai-token")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ssm unavailable")
}

func TestFetchAPIKey_NilGetter(t *testing.T) {
	_, err := fetchAPIKeyFromParamStore(context.Background(), nil, "/portfolio-agent/open-ai-token")
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestFetchAPIKey_EmptyName(t *testing.T) {
	g := &fakeGetter{val: `{"token":"sk-from-json"}`}
	_, err := fetchAPIKeyFromParamStore(context.Background(), g, " ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

// ---------------------------------------------------------------------------
// Client.Chat
// ---------------------------------------------------------------------------

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(
		&fakeGetter{val: `{"token":"sk-test"}`},
		"/portfolio-agent",
		WithBaseURL(srv.URL),
		WithHTTPClient(&http.Client{Timeout: 2 * time.Second}),
	)
	require.NoError(t, err)
	return c
}

func TestClient_Chat_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		reqBody, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(reqBody), `"response_format":{"type":"json_schema"`)
		require.Contains(t, string(reqBody), `"name":"scoped_answer"`)
		require.Contains(t, string(reqBody), `"required":["in_scope","answer"]`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-123",
			"object": "chat.completion",
			"created": 1670000000,
			"choices": [{
				"index": 0,
				"message": { "role": "assistant", "content": "Hello from mock" }
			}]
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.Chat(context.Background(), "gpt-mock", []domain.ChatMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	require.Equal(t, "Hello from mock", resp)
}

func TestClient_Chat_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Chat(context.Background(), "gpt-mock", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected status")
	require.Contains(t, err.Error(), "400")
}

func TestClient_Chat_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`not-a-json`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Chat(context.Background(), "gpt-mock", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode response")
}

func TestClient_Chat_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.httpClient = &http.Client{Timeout: 50 * time.Millisecond}
	_, err := c.Chat(context.Background(), "gpt-mock", nil)
	require.Error(t, err)
}

func TestClient_Chat_EmptyModel(t *testing.T) {
	c, err := NewClient(&fakeGetter{val: `{"token":"sk-test"}`}, "/portfolio-agent")
	require.NoError(t, err)
	_, err = c.Chat(context.Background(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "model")
}

func TestClient_Moderate_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":false}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.httpClient = &http.Client{Timeout: 50 * time.Millisecond}
	_, err := c.Moderate(context.Background(), "hello")
	require.Error(t, err)
}

func TestClient_Moderate_NetworkError(t *testing.T) {
	c, err := NewClient(&fakeGetter{val: `{"token":"sk-test"}`}, "/portfolio-agent")
	require.NoError(t, err)
	c.baseURL = "http://127.0.0.1:1"
	c.httpClient = &http.Client{Timeout: 100 * time.Millisecond}

	_, err = c.Moderate(context.Background(), "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "request failed")
}

func TestClient_Chat_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Chat(context.Background(), "gpt-mock", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no choices")
}

func TestClient_Chat_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Chat(context.Background(), "gpt-mock", []domain.ChatMessage{{Role: "user", Content: "hi"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "429")
}

func TestClient_Chat_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Chat(context.Background(), "gpt-mock", []domain.ChatMessage{{Role: "user", Content: "hi"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

// ---------------------------------------------------------------------------
// moderationURL helper
// ---------------------------------------------------------------------------

func TestModerationURL(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1/moderations"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/moderations"},
		{"http://localhost:8080", "http://localhost:8080/v1/moderations"},
		{"", "https://api.openai.com/v1/moderations"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, moderationURL(tc.base), "base=%q", tc.base)
	}
}

// ---------------------------------------------------------------------------
// Client.Moderate
// ---------------------------------------------------------------------------

func TestClient_Moderate_NotFlagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/moderations", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":false}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	flagged, err := c.Moderate(context.Background(), "What technologies do you use?")
	require.NoError(t, err)
	require.False(t, flagged)
}

func TestClient_Moderate_Flagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":true}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	flagged, err := c.Moderate(context.Background(), "some unsafe content")
	require.NoError(t, err)
	require.True(t, flagged)
}

func TestClient_Moderate_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Moderate(context.Background(), "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "429")
}

func TestClient_Moderate_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Moderate(context.Background(), "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

func TestClient_Moderate_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Moderate(context.Background(), "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode moderation response")
}

func TestClient_Moderate_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Moderate(context.Background(), "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no results")
}
