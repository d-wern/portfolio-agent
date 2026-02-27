package domain

// Message is a single persisted conversation turn.
type Message struct {
	PK             string
	SK             string
	ConversationID string
	Text           string
	Answer         string
	Tokens         int
	Status         string
	TTL            int64
}

// ConversationMeta stores aggregate conversation state.
type ConversationMeta struct {
	PK             string
	SK             string
	ConversationID string
	LastActivity   string
	Turns          int
	TTL            int64
}
