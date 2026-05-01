package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRetries = 3

// Client is a thin HTTP wrapper for any OpenAI-compatible chat completions API.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewClient creates a new LLM client.
func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends a system + user message pair and returns the assistant's reply.
func (c *Client) Chat(system, user string) (string, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	url += "chat/completions"

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := 5
			if h := resp.Header.Get("Retry-After"); h != "" {
				if r, _ := strconv.Atoi(h); r > 0 {
					retryAfter = r
				}
			}
			time.Sleep(time.Duration(retryAfter) * time.Second)
			lastErr = fmt.Errorf("rate limited")
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
			if resp.StatusCode >= 500 {
				continue
			}
			return "", lastErr
		}

		var result chatResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", fmt.Errorf("failed to unmarshal response: %w", err)
		}

		if result.Error != nil {
			return "", fmt.Errorf("API error: %s", result.Error.Message)
		}

		if len(result.Choices) == 0 {
			return "", fmt.Errorf("no choices in response")
		}

		return result.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("max retries exceeded: %w", lastErr)
}
