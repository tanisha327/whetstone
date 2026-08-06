package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Default wiring for the OpenAI-compatible client. Every one of these is
// overridable, because the same wire format is spoken by proxies, brokers, and
// self-hosted gateways.
const (
	// DefaultBaseURL is the public OpenAI API root.
	DefaultBaseURL = "https://api.openai.com/v1"
	// DefaultModel is used when no model is configured. Override it: the model
	// you have access to is an account-level fact this code cannot know.
	DefaultModel = "gpt-4o-mini"
	// DefaultTimeout bounds a single completion.
	DefaultTimeout = 90 * time.Second

	// maxResponseBytes caps how much of a response body we will read. A
	// provider is a remote host; an unbounded io.ReadAll from one is a
	// memory-exhaustion vector.
	maxResponseBytes = 8 << 20 // 8 MiB
)

// OpenAI speaks the OpenAI Chat Completions wire format.
//
// net/http rather than a vendor SDK: the surface used here is three JSON fields
// wide. Set BaseURL to point it at any OpenAI-compatible endpoint.
type OpenAI struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// OpenAIConfig configures NewOpenAI. Empty fields fall back to the matching
// environment variable, then to the package default.
type OpenAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
	// HTTPClient overrides the transport. Tests inject an httptest server here.
	HTTPClient *http.Client
}

// NewOpenAI returns a Provider backed by an OpenAI-compatible endpoint.
//
// It returns ErrNoCredential if no API key is available from any source, so the
// caller can print setup instructions instead of failing on the first request
// with an opaque 401.
func NewOpenAI(cfg OpenAIConfig) (*OpenAI, error) {
	key, err := loadKey(cfg.APIKey)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &OpenAI{
		apiKey:  key,
		baseURL: normalizeBaseURL(firstNonEmpty(cfg.BaseURL, os.Getenv(EnvBaseURL), DefaultBaseURL)),
		model:   firstNonEmpty(cfg.Model, os.Getenv(EnvModel), DefaultModel),
		http:    client,
	}, nil
}

// Name implements Provider.
func (o *OpenAI) Name() string { return "openai" }

// Model reports the configured model, for display in the status bar.
func (o *OpenAI) Model() string { return o.model }

// BaseURL reports the configured endpoint, for display in the status bar.
func (o *OpenAI) BaseURL() string { return o.baseURL }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	Temperature    *float64      `json:"temperature,omitempty"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
}

type respFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete implements Provider.
func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	if err := validate(req); err != nil {
		return Response{}, err
	}

	msgs := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: string(m.Role), Content: m.Text})
	}

	body := chatRequest{Model: o.model, Messages: msgs, MaxTokens: req.MaxTokens}
	if req.Temperature != 0 {
		t := req.Temperature
		body.Temperature = &t
	}
	if req.JSON {
		body.ResponseFormat = &respFormat{Type: "json_object"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("provider: encoding %s request: %w", req.Purpose, err)
	}

	url := o.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("provider: building %s request: %w", req.Purpose, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("provider: %s request: %w", req.Purpose, o.scrub(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Response{}, fmt.Errorf("provider: reading %s response: %w", req.Purpose, err)
	}

	var parsed chatResponse
	// Decode before checking the status: error responses carry a useful body.
	decodeErr := json.Unmarshal(raw, &parsed)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(raw))
		if decodeErr == nil && parsed.Error != nil && parsed.Error.Message != "" {
			detail = parsed.Error.Message
		}
		return Response{}, fmt.Errorf("provider: %s returned %d: %s",
			req.Purpose, resp.StatusCode, o.redact(truncate(detail, 500)))
	}
	if decodeErr != nil {
		return Response{}, fmt.Errorf("provider: decoding %s response: %w", req.Purpose, decodeErr)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("provider: %s response contained no choices", req.Purpose)
	}

	model := parsed.Model
	if model == "" {
		model = o.model
	}
	return Response{
		Text:  parsed.Choices[0].Message.Content,
		Model: model,
		Usage: Usage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
		},
	}, nil
}

// redact strips the API key from text that may end up in a log or on screen.
func (o *OpenAI) redact(s string) string { return redact(s, o.apiKey) }

// scrub wraps an error so its message cannot leak the key. Some transports
// include the request URL, and a misconfigured base URL could embed one.
func (o *OpenAI) scrub(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if clean := o.redact(msg); clean != msg {
		return fmt.Errorf("%s", clean)
	}
	return err
}

// normalizeBaseURL trims trailing slashes so path joins cannot produce "//".
// Normalising once here is deliberate: doing it at each call site is how you
// end up with three sites that do and four that do not.
func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
