// Package provider is the only package that knows a model vendor exists.
//
// Everything above it depends on the Provider interface, so swapping vendors
// touches one file and the API key is handled in one place.
//
// The shipped implementation is OpenAI. There is no mock and no offline mode;
// tests use small fakes in their own _test.go files.
package provider

import (
	"context"
	"errors"
	"strings"
)

// Role identifies who authored a Message.
type Role string

const (
	// RoleUser is the operator-authored turn.
	RoleUser Role = "user"
	// RoleAssistant is a prior model turn, replayed for context.
	RoleAssistant Role = "assistant"
)

// Message is one turn of context handed to the model.
type Message struct {
	Role Role
	Text string
}

// Purpose labels a Request by the feature that issued it. Never sent to the
// model: it tags errors and enumerates every way this program may talk to one.
// Required on every request.
type Purpose string

const (
	// PurposeLens asks for a task-relevant micro-summary of one section.
	PurposeLens Purpose = "lens"
	// PurposeProvokeSection asks for critiques of a passage the user is reading.
	PurposeProvokeSection Purpose = "provoke.section"
	// PurposeProvokeOutline asks for critiques of the user's argument structure.
	PurposeProvokeOutline Purpose = "provoke.outline"
	// PurposeDraft turns an outline node plus its grounding into prose.
	PurposeDraft Purpose = "draft"
	// PurposeRewrite asks for alternative phrasings of the author's own prose
	// along a chosen dimension. It never invents content.
	PurposeRewrite Purpose = "rewrite"
	// PurposeCompose turns the author's own instruction, notes, and citations
	// into a paragraph. The instruction is scoped to one outline point; it is
	// not a general prompt box. See docs/adr/0001-no-chat-box.md.
	PurposeCompose Purpose = "compose"
	// PurposeCheck is the minimal round-trip issued by -check to verify that
	// the key, endpoint, and model actually work together.
	PurposeCheck Purpose = "check"
)

// Request is a single completion request. Zero values mean "provider default"
// for MaxTokens and Temperature.
type Request struct {
	// Purpose identifies the calling feature. Required.
	Purpose Purpose
	// System is the instruction preamble.
	System string
	// Messages is the conversation, oldest first. Must be non-empty.
	Messages []Message
	// MaxTokens caps the response length.
	MaxTokens int
	// Temperature is the sampling temperature. Whetstone deliberately runs
	// provocations warm and summaries cold.
	Temperature float64
	// JSON requests a strict JSON object response. Callers that set this must
	// still tolerate a parse failure: no provider guarantees well-formed output.
	JSON bool
}

// Response is a completed request.
type Response struct {
	// Text is the model's reply. For a JSON request this is the raw object.
	Text string
	// Model is the concrete model that served the request.
	Model string
	// Usage is best-effort; a provider that reports nothing leaves it zero.
	Usage Usage
}

// Usage is the token accounting for one call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Provider turns a Request into a Response.
//
// Implementations must be safe for concurrent use: the TUI issues provocation
// requests for several sections at once.
type Provider interface {
	// Name is a short identifier for display, e.g. "openai".
	Name() string
	// Complete performs one request. It must honour ctx cancellation.
	Complete(ctx context.Context, req Request) (Response, error)
}

// ErrEmptyRequest is returned for a Request with no Messages.
var ErrEmptyRequest = errors.New("provider: request has no messages")

// validate applies the invariants every implementation depends on, so each
// provider does not repeat them.
func validate(req Request) error {
	if len(req.Messages) == 0 {
		return ErrEmptyRequest
	}
	if req.Purpose == "" {
		return errors.New("provider: request has no purpose")
	}
	return nil
}

// redact removes a secret from a string so credentials cannot reach a log line,
// an error message, or the screen. It is a no-op for an empty secret, so a
// caller holding no key does not replace every empty substring in the input.
func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***REDACTED***")
}
