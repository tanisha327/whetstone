package provider

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExtractJSON finds a JSON object in a model response and unmarshals it into v.
//
// Request.JSON asks for a bare object but nothing guarantees one: models add
// ```json fences, a "Here is the JSON:" preamble, or a trailing sentence. Every
// JSON-shaped feature goes through here rather than tolerating that itself.
//
// Errors quote a snippet of what came back, because "invalid character 'H'"
// alone tells you nothing.
func ExtractJSON(raw string, v any) error {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return fmt.Errorf("provider: empty response where JSON was expected")
	}

	if fenced, ok := stripFence(candidate); ok {
		candidate = fenced
	}
	if obj, ok := firstObject(candidate); ok {
		candidate = obj
	}

	if err := json.Unmarshal([]byte(candidate), v); err != nil {
		return fmt.Errorf("provider: response was not valid JSON (%w): %s",
			err, truncate(strings.TrimSpace(raw), 300))
	}
	return nil
}

// stripFence removes a leading ```lang / trailing ``` wrapper.
func stripFence(s string) (string, bool) {
	if !strings.HasPrefix(s, "```") {
		return s, false
	}
	rest := s[3:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return s, false
	}
	if end := strings.LastIndex(rest, "```"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest), true
}

// firstObject returns the outermost {...} span, skipping any prose around it.
// Brace counting is string-aware so a "}" inside a quoted value does not close
// the object early.
func firstObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return s, false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return s, false
}
