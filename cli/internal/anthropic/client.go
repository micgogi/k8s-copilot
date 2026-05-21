// Package anthropic is a minimal HTTP client for the Claude Messages API.
//
// We use raw HTTP rather than the official Go SDK for two reasons:
//   - The SDK has been moving fast; pinning to raw HTTP avoids version drift.
//   - We only need a tiny slice of the API (messages + tools + prompt caching).
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	endpoint   = "https://api.anthropic.com/v1/messages"
	apiVersion = "2023-06-01"
)

// CacheControl marks a block as cacheable. Only "ephemeral" is supported today.
type CacheControl struct {
	Type string `json:"type"`
}

// Ephemeral returns a *CacheControl{"ephemeral"} — used to mark cacheable blocks.
func Ephemeral() *CacheControl { return &CacheControl{Type: "ephemeral"} }

// ContentBlock represents a single block of a message or response.
// It is a union of all block types we care about: text, tool_use, tool_result.
type ContentBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// Message is a single conversational turn.
type Message struct {
	Role    string         `json:"role"` // "user" or "assistant"
	Content []ContentBlock `json:"content"`
}

// SystemBlock is one chunk of the system prompt. Put cache_control on the last
// one to cache the whole prefix.
type SystemBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// Tool is a tool definition exposed to the model.
type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
}

// Request is a Messages API request.
type Request struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    []SystemBlock `json:"system,omitempty"`
	Tools     []Tool        `json:"tools,omitempty"`
	Messages  []Message     `json:"messages"`
}

// Usage reports token consumption. Cache fields are nonzero when prompt
// caching is engaged — useful for verifying caching is working.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// Response is a Messages API response.
type Response struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

// Client is a Claude Messages API client.
type Client struct {
	apiKey string
	http   *http.Client
}

// NewClient builds a Client. apiKey must be non-empty.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

// Send issues a single Messages API request.
func (c *Client) Send(ctx context.Context, req Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build http request: %w", err)
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Set("content-type", "application/json")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API %d: %s", httpResp.StatusCode, string(respBody))
	}

	var out Response
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
	}
	return &out, nil
}
