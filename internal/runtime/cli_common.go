package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

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

var Runner ExecRunner = &defaultRunner{}

func runCLI(ctx context.Context, cmd string, args ...string) (string, error) {
	out, err := Runner.Run(ctx, cmd, args...)
	if err != nil {
		return "", fmt.Errorf("run cli: %w", err)
	}
	return out, nil
}

func parseJSONList(src string) ([]map[string]any, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal([]byte(src), &v); err != nil {
		return parseNewlineSeparatedJSON(src)
	}
	return parseJSONArray(v)
}

func parseNewlineSeparatedJSON(src string) ([]map[string]any, error) {
	lines := strings.Split(src, "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			return nil, fmt.Errorf("could not parse line as JSON: %w\nline: %s", err, l)
		}
		out = append(out, m)
	}
	return out, nil
}

func parseJSONArray(v any) ([]map[string]any, error) {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out, nil
	}
	if m, ok := v.(map[string]any); ok {
		return []map[string]any{m}, nil
	}
	return nil, errors.New("unexpected json shape")
}

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
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

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

func unmarshalRepoTags(raw any) []string {
	if raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case []any:
		return extractStrings(v)
	case []string:
		return v
	case string:
		return parseStringRepoTags(v)
	default:
		return tryMarshalRoundtrip(v)
	}
}

func extractStrings(v []any) []string {
	out := make([]string, 0, len(v))
	for _, x := range v {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func parseStringRepoTags(v string) []string {
	s := strings.TrimSpace(v)
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr
	}
	if s == "" {
		return nil
	}
	return []string{s}
}

func tryMarshalRoundtrip(v any) []string {
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

func parseTimeRFC3339(s string) int64 {
	if s == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", s); err == nil {
		return t.Unix()
	}
	return 0
}
