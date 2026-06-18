package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// ─── Environment Variables ───────────────────────────────

const (
	EnvAPIKey    = "LLM_API_KEY"
	EnvBaseURL   = "LLM_BASE_URL"
	EnvModel     = "LLM_MODEL"
	DefaultModel = "gpt-4o-mini"
)

// ─── OpenAI API Types ────────────────────────────────────

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	Temperature    float64               `json:"temperature,omitempty"`
	Stream         bool                  `json:"stream,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIResponseFormat struct {
	Type       string       `json:"type"`
	JSONSchema *openAISchema `json:"json_schema,omitempty"`
}

type openAISchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIStreamDelta struct {
	Content string `json:"content,omitempty"`
}

type openAIStreamChoice struct {
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openAIStreamChunk struct {
	Choices []openAIStreamChoice `json:"choices"`
}

// ─── Client Implementation ───────────────────────────────

// OpenAIClient implements LLMClient for OpenAI-compatible APIs.
// Supports OpenAI, DeepSeek, Qwen, Claude via API proxy, and any
// service using the OpenAI chat completions protocol.
type OpenAIClient struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewOpenAIClient creates an OpenAIClient from environment variables.
//
// Environment variables:
//   - LLM_API_KEY (fallback: OPENAI_API_KEY)
//   - LLM_BASE_URL (default: https://api.openai.com/v1)
//   - LLM_MODEL (default: gpt-4o-mini)
func NewOpenAIClient() *OpenAIClient {
	apiKey := os.Getenv(EnvAPIKey)
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	baseURL := os.Getenv(EnvBaseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	model := os.Getenv(EnvModel)
	if model == "" {
		model = DefaultModel
	}

	return &OpenAIClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		model:      model,
		httpClient: &http.Client{},
	}
}

// NewOpenAIClientWithConfig creates an OpenAIClient with explicit configuration.
func NewOpenAIClientWithConfig(apiKey, baseURL, model string) *OpenAIClient {
	return &OpenAIClient{
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		httpClient: &http.Client{},
	}
}

// Model returns the configured model name.
func (c *OpenAIClient) Model() string {
	return c.model
}

// Chat implements LLMClient.Chat.
func (c *OpenAIClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}

	openAIReq := c.buildRequest(req, false)

	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	slog.Debug("llm request",
		"model", model,
		"messages", len(req.Messages),
		"body_size", len(body),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm: API error (HTTP %d): %s",
			resp.StatusCode, truncateString(string(respBody), 500))
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("llm: parse response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty response (no choices)")
	}

	result := &ChatResponse{
		Content: strings.TrimSpace(openAIResp.Choices[0].Message.Content),
		Usage: Usage{
			PromptTokens:     openAIResp.Usage.PromptTokens,
			CompletionTokens: openAIResp.Usage.CompletionTokens,
			TotalTokens:      openAIResp.Usage.TotalTokens,
		},
	}

	slog.Debug("llm response",
		"content_length", len(result.Content),
		"prompt_tokens", result.Usage.PromptTokens,
		"completion_tokens", result.Usage.CompletionTokens,
	)

	return result, nil
}

// ChatStream implements LLMClient.ChatStream.
func (c *OpenAIClient) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	openAIReq := c.buildRequest(req, true)

	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}

	c.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: http request: %w", err)
	}

	ch := make(chan StreamEvent, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			ch <- StreamEvent{
				Error: fmt.Errorf("llm: API error (HTTP %d): %s",
					resp.StatusCode, truncateString(string(bodyBytes), 500)),
			}
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- StreamEvent{Done: true}
				return
			}

			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				slog.Debug("llm: skip parse chunk", "error", err)
				continue
			}

			if len(chunk.Choices) > 0 {
				content := chunk.Choices[0].Delta.Content
				if content != "" {
					ch <- StreamEvent{Content: content}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamEvent{Error: fmt.Errorf("llm: stream read: %w", err)}
		}
	}()

	return ch, nil
}

// ─── Factory ─────────────────────────────────────────────

// NewDefaultClient creates an LLMClient using environment configuration.
// Returns an error if the API key is not configured.
func NewDefaultClient() (LLMClient, error) {
	client := NewOpenAIClient()
	if client.apiKey == "" {
		return nil, fmt.Errorf("llm: %s is not set (set env var or LLM_API_KEY)", EnvAPIKey)
	}
	return client, nil
}

// ─── Internal Helpers ─────────────────────────────────────

func (c *OpenAIClient) buildRequest(req ChatRequest, stream bool) openAIRequest {
	openAIReq := openAIRequest{
		Model:       c.model,
		Messages:    make([]openAIMessage, len(req.Messages)),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      stream,
	}

	if openAIReq.MaxTokens == 0 {
		openAIReq.MaxTokens = 4096
	}
	if openAIReq.Temperature == 0 {
		openAIReq.Temperature = 0.1
	}

	for i, msg := range req.Messages {
		openAIReq.Messages[i] = openAIMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}

	if req.Schema != nil {
		openAIReq.ResponseFormat = &openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: &openAISchema{
				Name:   "structured_response",
				Schema: req.Schema,
				Strict: true,
			},
		}
	}

	return openAIReq
}

func (c *OpenAIClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}
