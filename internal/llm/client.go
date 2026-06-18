// Package llm provides a model-agnostic abstraction layer for LLM interactions.
package llm

import "context"

// ─── Role ─────────────────────────────────────────────────

// Role represents a message role in a chat conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ─── Request / Response Types ─────────────────────────────

// Message represents a single message in a chat conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatRequest contains parameters for a chat completion request.
type ChatRequest struct {
	Model       string    `json:"model,omitempty"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	// Schema is an optional JSON Schema for structured output.
	// If non-nil, the response will be constrained to this schema.
	Schema map[string]any `json:"-"`
}

// Usage contains token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse contains the response from a chat completion request.
type ChatResponse struct {
	Content string
	Usage   Usage
}

// StreamEvent represents a single event in a streaming chat completion.
type StreamEvent struct {
	Content string
	Done    bool
	Error   error
}

// ─── Interface ───────────────────────────────────────────

// LLMClient defines the interface for LLM interactions.
type LLMClient interface {
	// Chat sends a chat completion request and returns the response.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// ChatStream sends a streaming chat completion request.
	// Returns a channel of events. The caller must drain the channel.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
