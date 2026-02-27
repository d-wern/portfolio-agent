package domain

// ChatMessage is the provider-agnostic chat message shape used by the handler
// and LLM integrations.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
