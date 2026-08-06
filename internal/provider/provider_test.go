package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     Request
		wantErr error
	}{
		{
			name:    "no messages",
			req:     Request{Purpose: PurposeLens},
			wantErr: ErrEmptyRequest,
		},
		{
			name: "ok",
			req: Request{
				Purpose:  PurposeLens,
				Messages: []Message{{Role: RoleUser, Text: "hi"}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(tc.req)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validate = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("validate = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// Purpose is the enumeration of every way this program may talk to a model.
// An unlabelled request means a new call path slipped in without one.
func TestValidate_RequiresPurpose(t *testing.T) {
	err := validate(Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err == nil {
		t.Fatal("expected an error for a request with no purpose")
	}
	if !strings.Contains(err.Error(), "purpose") {
		t.Errorf("error should mention the purpose, got: %v", err)
	}
}

func TestRedact(t *testing.T) {
	tests := []struct {
		in, secret, want string
	}{
		{"key is sk-abc123 here", "sk-abc123", "key is ***REDACTED*** here"},
		{"nothing to hide", "", "nothing to hide"},
		{"no match", "sk-zzz", "no match"},
		{"sk-a and sk-a", "sk-a", "***REDACTED*** and ***REDACTED***"},
	}
	for _, tc := range tests {
		if got := redact(tc.in, tc.secret); got != tc.want {
			t.Errorf("redact(%q, %q) = %q, want %q", tc.in, tc.secret, got, tc.want)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	type payload struct {
		Text string `json:"text"`
	}
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"bare object", `{"text":"hello"}`, "hello", false},
		{"fenced", "```json\n{\"text\":\"hello\"}\n```", "hello", false},
		{"fenced no lang", "```\n{\"text\":\"hello\"}\n```", "hello", false},
		{"prose prefix", `Here is the JSON: {"text":"hello"}`, "hello", false},
		{"prose suffix", `{"text":"hello"} Let me know if you need more.`, "hello", false},
		{"brace inside string", `{"text":"a } brace"}`, "a } brace", false},
		{"escaped quote", `{"text":"say \"hi\""}`, `say "hi"`, false},
		{"empty", "", "", true},
		{"not json", "I cannot help with that.", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p payload
			err := ExtractJSON(tc.raw, &p)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && p.Text != tc.want {
				t.Errorf("Text = %q, want %q", p.Text, tc.want)
			}
		})
	}
}

func TestExtractJSON_ErrorIncludesPayload(t *testing.T) {
	var p struct{}
	err := ExtractJSON("I refuse to answer that question.", &p)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "I refuse") {
		t.Errorf("error should quote the payload, got: %v", err)
	}
}
