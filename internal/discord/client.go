package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	DiscordAPIBase = "https://discord.com/api/v10"
	BatchSize      = 100
	MaxRetries     = 3
	CourtesyDelay  = 50 * time.Millisecond
)

type Client struct {
	token   string
	client  *http.Client
	verbose bool
}

type Message struct {
	ID              string       `json:"id"`
	Content         string       `json:"content"`
	Author          User         `json:"author"`
	Timestamp       string       `json:"timestamp"`
	ChannelID       string       `json:"channel_id"`
	Attachments     []Attachment `json:"attachments"`
	Embeds          []Embed      `json:"embeds"`
	Reactions       []Reaction   `json:"reactions"`
	ReferencedMessage *Message   `json:"referenced_message"`
	MessageReference *struct {
		MessageID string `json:"message_id"`
	} `json:"message_reference"`
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int    `json:"size"`
	ContentType string `json:"content_type"`
}

type Embed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Author      *struct {
		Name string `json:"name"`
	} `json:"author"`
	Footer *struct {
		Text string `json:"text"`
	} `json:"footer"`
}

type Reaction struct {
	Count int    `json:"count"`
	Emoji struct {
		Name string `json:"name"`
	} `json:"emoji"`
}

func NewClient(token string, verbose bool) *Client {
	return &Client{
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
		verbose: verbose,
	}
}

func (c *Client) FetchAllMessages(channelID string) ([]Message, error) {
	var allMessages []Message
	var beforeID string

	for {
		messages, err := c.fetchBatch(channelID, beforeID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch batch: %w", err)
		}

		if len(messages) == 0 {
			break
		}

		allMessages = append(allMessages, messages...)
		beforeID = messages[len(messages)-1].ID

		if c.verbose {
			fmt.Fprintf(os.Stderr, "  Fetched batch: %d messages (total: %d)\n", len(messages), len(allMessages))
		}

		// Courtesy delay to avoid hammering the API
		time.Sleep(CourtesyDelay)
	}

	return allMessages, nil
}

func (c *Client) fetchBatch(channelID, beforeID string) ([]Message, error) {
	url := fmt.Sprintf("%s/channels/%s/messages?limit=%d", DiscordAPIBase, channelID, BatchSize)
	if beforeID != "" {
		url = fmt.Sprintf("%s&before=%s", url, beforeID)
	}

	var messages []Message
	var lastErr error

	for attempt := 0; attempt < MaxRetries; attempt++ {
		if attempt > 0 {
			if c.verbose {
				fmt.Fprintf(os.Stderr, "  Retrying request (attempt %d/%d)...\n", attempt+1, MaxRetries)
			}
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// User token authentication (no "Bot " prefix)
		req.Header.Set("Authorization", c.token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}

		// Check rate limit headers
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		if remaining != "" {
			if rem, _ := strconv.Atoi(remaining); rem <= 1 {
				// We're close to the limit, wait a bit
				if c.verbose {
					fmt.Fprintf(os.Stderr, "  Rate limit running low, waiting...\n")
				}
				time.Sleep(500 * time.Millisecond)
			}
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		// Handle rate limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			var retryAfter int
			retryHeader := resp.Header.Get("Retry-After")
			if retryHeader != "" {
				retryAfter, _ = strconv.Atoi(retryHeader)
			}
			if retryAfter == 0 {
				retryAfter = 5 // Default to 5 seconds if header not present
			}

			if c.verbose {
				fmt.Fprintf(os.Stderr, "  Rate limited, waiting %d seconds...\n", retryAfter)
			}
			time.Sleep(time.Duration(retryAfter) * time.Second)
			lastErr = fmt.Errorf("rate limited")
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
			if resp.StatusCode >= 500 {
				// Server error, retry
				continue
			}
			return nil, lastErr
		}

		if err := json.Unmarshal(body, &messages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal messages: %w", err)
		}

		return messages, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (c *Client) DownloadAttachment(url string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			var retryAfter int
			retryHeader := resp.Header.Get("Retry-After")
			if retryHeader != "" {
				retryAfter, _ = strconv.Atoi(retryHeader)
			}
			if retryAfter == 0 {
				retryAfter = 5
			}
			time.Sleep(time.Duration(retryAfter) * time.Second)
			lastErr = fmt.Errorf("rate limited")
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			if resp.StatusCode >= 500 {
				continue
			}
			return nil, lastErr
		}

		return io.ReadAll(resp.Body)
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}
