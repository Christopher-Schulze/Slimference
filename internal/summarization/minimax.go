package summarization

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/tokenproxy/tokenproxy/internal/config"
)

const systemPrompt = `You are a lossless conversation compressor for AI coding sessions.
Your task: produce a dense, structured summary that lets an AI assistant continue the session
without re-reading the original messages.

Rules:
- Preserve EVERY file path, function name, error message, and decision verbatim.
- Preserve all anchor facts: what was edited, what failed, what was approved.
- Use terse bullet points; no prose filler.
- Do NOT omit technical details. Omit only greetings, pleasantries, and repeated context.
- Output plain text only - no markdown headers, no code fences around the summary itself.
- Start immediately with the first fact. No preamble.`

// mmRequest is the OpenAI-compatible chat completion request body.
type mmRequest struct {
	Model       string      `json:"model"`
	Messages    []mmMessage `json:"messages"`
	MaxTokens   int         `json:"max_tokens"`
	Temperature float64     `json:"temperature"`
	Stream      bool        `json:"stream"`
}

// mmMessage is a single role/content pair used in the chat completion API.
type mmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// mmResponse is the OpenAI-compatible chat completion response envelope.
type mmResponse struct {
	Choices []mmChoice `json:"choices"`
}

// mmChoice wraps a single completion candidate.
type mmChoice struct {
	Message mmMessage `json:"message"`
}

// MiniMaxClient calls the MiniMax M2.7 API for abstractive summarization.
type MiniMaxClient struct {
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	maxRetries  int
	httpClient  *http.Client
	limiter     *rate.Limiter
}

// NewMiniMaxClient builds a MiniMaxClient from the supplied configuration.
func NewMiniMaxClient(cfg config.MiniMaxConfig) *MiniMaxClient {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: cfg.ConnectTimeout(),
		}).DialContext,
		ResponseHeaderTimeout: cfg.ResponseTimeout(),
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.ConnectTimeout() + cfg.ResponseTimeout(),
	}

	rpm := cfg.RateLimitRPM
	if rpm <= 0 {
		rpm = 10
	}
	// Convert RPM to per-second rate with a burst of 1.
	rps := rate.Limit(float64(rpm) / 60.0)
	limiter := rate.NewLimiter(rps, 1)

	return &MiniMaxClient{
		baseURL:     cfg.BaseURL,
		apiKey:      cfg.APIKey(),
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxRetries:  cfg.MaxRetries,
		httpClient:  httpClient,
		limiter:     limiter,
	}
}

// IsConfigured reports whether the client has an API key configured.
func (c *MiniMaxClient) IsConfigured() bool {
	return c.apiKey != ""
}

// Summarize sends the inputText to MiniMax and returns the compressed summary.
// It retries up to MaxRetries times on 429 or 5xx responses with exponential backoff.
func (c *MiniMaxClient) Summarize(inputText string, startMsg, endMsg, targetTokens int) (string, error) {
	_ = c.limiter.Wait(context.Background())

	userContent := fmt.Sprintf(
		"Compress this conversation segment (messages %d to %d). Target: %d tokens maximum.\n\nCONVERSATION:\n%s",
		startMsg, endMsg, targetTokens, inputText,
	)

	payload := mmRequest{
		Model: c.model,
		Messages: []mmMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		MaxTokens:   targetTokens,
		Temperature: c.temperature,
		Stream:      false,
	}

	backoff := []time.Duration{1 * time.Second, 3 * time.Second}
	maxAttempts := c.maxRetries + 1

	var lastErr error
	for attempt := range maxAttempts {
		summary, err := c.doRequest(payload)
		if err == nil {
			return summary, nil
		}

		lastErr = err

		// Only retry on transient errors.
		if !isRetryable(err) {
			break
		}

		if attempt < len(backoff) {
			time.Sleep(backoff[attempt])
		}
	}

	return "", fmt.Errorf("minimax summarize failed after %d attempts: %w", maxAttempts, lastErr)
}

// doRequest executes a single HTTP call and returns the summary text or a typed error.
func (c *MiniMaxClient) doRequest(payload mmRequest) (string, error) {
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", &retryableError{cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", &retryableError{
			cause: fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(respBody), 200)),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var parsed mmResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty response from minimax")
	}

	return parsed.Choices[0].Message.Content, nil
}

// retryableError marks an error that should trigger a retry.
type retryableError struct {
	cause error
}

func (e *retryableError) Error() string { return e.cause.Error() }
func (e *retryableError) Unwrap() error { return e.cause }

// isRetryable returns true if the error is a *retryableError.
func isRetryable(err error) bool {
	_, ok := err.(*retryableError)
	return ok
}

// truncate clips a string to at most n bytes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
