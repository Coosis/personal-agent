package agenthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	// "os"
	"strings"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type ChatRequest struct {
	Content  string        `json:"content"`
	Messages []ChatMessage `json:"messages,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type ChatStreamResult struct {
	Citations []byte
}

type streamChunk struct {
	Type      string          `json:"type"`
	Content   string          `json:"content"`
	Citations json.RawMessage `json:"citations"`
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		// SSE chat streaming should rely on request context rather than
		// a total http.Client timeout, which aborts long responses mid-stream.
		httpClient: &http.Client{},
	}
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("build health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call health endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent health failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func (c *Client) Chat(ctx context.Context, content string, messages []ChatMessage) (string, error) {
	var contentBuilder strings.Builder
	_, err := c.ChatStream(ctx, content, messages, func(token string) error {
		contentBuilder.WriteString(token)
		return nil
	})
	if err != nil {
		return "", err
	}
	return contentBuilder.String(), nil
}

// repeatedly blocks and reads from stream, invoking onToken callback for each new token received until
// stream ends or context is canceled. If onToken is nil, tokens will be discarded.
func (c *Client) ChatStream(
	ctx context.Context,
	content string, // the message content to send to the agent
	messages []ChatMessage,
	onToken func(string) error,
) (ChatStreamResult, error) {
	body, err := json.Marshal(ChatRequest{
		Content:  content,
		Messages: messages,
	})
	if err != nil {
		return ChatStreamResult{}, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/stream", bytes.NewReader(body))
	if err != nil {
		return ChatStreamResult{}, fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ChatStreamResult{}, fmt.Errorf("call chat endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var failure errorResponse
		if err := json.Unmarshal(raw, &failure); err == nil && failure.Error != "" {
			return ChatStreamResult{}, fmt.Errorf("agent chat failed: %s", failure.Error)
		}
		return ChatStreamResult{}, fmt.Errorf("agent chat failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	result := ChatStreamResult{Citations: []byte("[]")}
	err = consumeSSE(resp.Body, func(event sseEvent) error {
		if event.Data == "" {
			return nil
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("decode sse data: %w", err)
		}

		if chunk.Type == "stop" && len(chunk.Citations) > 0 {
			result.Citations = []byte(chunk.Citations)
			return nil
		}

		if chunk.Type != "token" {
			return nil
		}

		// if _, err := os.Stdout.WriteString(chunk.Content); err != nil {
		// 	return fmt.Errorf("write token to stdout: %w", err)
		// }
		if onToken == nil {
			return nil
		}
		return onToken(chunk.Content)
	})
	if err != nil {
		return ChatStreamResult{}, fmt.Errorf("read chat stream: %w", err)
	}

	return result, nil
}
