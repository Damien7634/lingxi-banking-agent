package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Minimal OpenAI-compatible chat-completions client with streaming tool calls
// (works with DeepSeek, Kimi, Qwen, OpenAI gateways).

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type toolSpec struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type llmClient struct {
	baseURL, apiKey, model string
	http                   *http.Client
}

// ErrUnavailable: quota exhausted / payment required — do not retry.
var ErrUnavailable = errors.New("llm unavailable")

// ErrMisconfigured: bad credentials or endpoint.
var ErrMisconfigured = errors.New("llm misconfigured")

type completion struct {
	Text      string
	ToolCalls []ToolCall
}

func (c *llmClient) stream(ctx context.Context, msgs []Message, tools []toolSpec, onText func(string) error) (*completion, error) {
	body, err := json.Marshal(map[string]any{
		"model":       c.model,
		"messages":    msgs,
		"tools":       tools,
		"stream":      true,
		"temperature": 0.3,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		switch resp.StatusCode {
		case http.StatusPaymentRequired:
			return nil, ErrUnavailable
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return nil, fmt.Errorf("%w: HTTP %d", ErrMisconfigured, resp.StatusCode)
		}
		if bytes.Contains(bytes.ToLower(snippet), []byte("insufficient")) {
			return nil, ErrUnavailable
		}
		return nil, fmt.Errorf("llm HTTP %d: %s", resp.StatusCode, snippet)
	}

	out := &completion{}
	calls := map[int]*ToolCall{}
	var order []int
	var text strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Content != "" {
			text.WriteString(d.Content)
			if err := onText(d.Content); err != nil {
				return nil, err
			}
		}
		for _, tc := range d.ToolCalls {
			cur, ok := calls[tc.Index]
			if !ok {
				cur = &ToolCall{Type: "function"}
				calls[tc.Index] = cur
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			cur.Function.Name += tc.Function.Name
			cur.Function.Arguments += tc.Function.Arguments
			if len(cur.Function.Arguments) > 16<<10 {
				return nil, errors.New("llm tool arguments too large")
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("llm stream: %w", err)
	}
	out.Text = text.String()
	for i, idx := range order {
		tc := calls[idx]
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
		}
		out.ToolCalls = append(out.ToolCalls, *tc)
	}
	return out, nil
}
