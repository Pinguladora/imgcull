package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExecRunner allows adapters to inject a runner for tests.
type ExecRunner interface {
	Run(ctx context.Context, cmd string, args ...string) (string, error)
}

type defaultRunner struct{}

func (r *defaultRunner) Run(ctx context.Context, cmd string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, cmd, args...)
	out, err := c.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v: %w\n%s", cmd, args, err, string(out))
	}
	return string(out), nil
}

// Runner is used by adapters; tests can replace it with a fake.
var Runner ExecRunner = &defaultRunner{}

// runCLI calls the configured Runner.
func runCLI(ctx context.Context, cmd string, args ...string) (string, error) {
	return Runner.Run(ctx, cmd, args...)
}

// parseJSONList parses either a JSON array or newline-separated JSON objects.
// Returns a slice of maps (each map = one JSON object).
func parseJSONList(src string) ([]map[string]any, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal([]byte(src), &v); err != nil {
		// try newline separated JSON objects
		lines := strings.Split(src, "\n")
		out := make([]map[string]any, 0, len(lines))
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(l), &m); err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		return out, nil
	}
	// if it's already an array
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out, nil
	}
	// if it's a single object
	if m, ok := v.(map[string]any); ok {
		return []map[string]any{m}, nil
	}
	return nil, fmt.Errorf("unexpected json shape")
}

// string conversion helper
func toString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case json.Number:
		return t.String()
	case float64:
		// JSON numbers are float64 by default; convert without trailing .0 if possible
		s := fmt.Sprintf("%v", t)
		return s
	default:
		return fmt.Sprintf("%v", t)
	}
}

// toInt64 handles common numeric shapes from decoded JSON.
func toInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
	}
	return 0
}

// unmarshalRepoTags deals with various shapes for RepoTags/Repo field: []string, JSON string, or single string.
func unmarshalRepoTags(raw any) []string {
	if raw == nil {
		return nil
	}
	// if it's already []interface{}
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		// sometimes adapters pass JSON-encoded string
		s := strings.TrimSpace(v)
		// try parse as JSON array
		var arr []string
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			return arr
		}
		// fallback: single value
		if s == "" {
			return nil
		}
		return []string{s}
	default:
		// try marshal/unmarshal roundtrip
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var arr []string
		if err := json.Unmarshal(b, &arr); err == nil {
			return arr
		}
		return nil
	}
}

// parseTimeRFC3339 returns unix seconds or 0.
func parseTimeRFC3339(s string) int64 {
	if s == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	// tolerate slight variants
	if t, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", s); err == nil {
		return t.Unix()
	}
	return 0
}
