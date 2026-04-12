package runtime

import (
	"context"
	"errors"
	"testing"
)

type mockExecRunner struct {
	output  string
	err     error
	gotCmd  string
	gotArgs []string
}

func (m *mockExecRunner) Run(_ context.Context, cmd string, args ...string) (string, error) {
	m.gotCmd = cmd
	m.gotArgs = args
	return m.output, m.err
}

func withMockRunner(m *mockExecRunner, fn func()) {
	old := Runner
	Runner = m
	defer func() { Runner = old }()
	fn()
}

func TestParseJSONList_Array(t *testing.T) {
	input := `[{"ID":"img1","Size":100},{"ID":"img2","Size":200}]`
	got, err := parseJSONList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if toString(got[0]["ID"]) != "img1" {
		t.Errorf("got[0][ID] = %v, want img1", got[0]["ID"])
	}
}

func TestParseJSONList_NewlineSeparated(t *testing.T) {
	input := "{\"ID\":\"img1\",\"Size\":100}\n{\"ID\":\"img2\",\"Size\":200}\n"
	got, err := parseJSONList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestParseJSONList_SingleObject(t *testing.T) {
	input := `{"ID":"img1","Size":100}`
	got, err := parseJSONList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestParseJSONList_Empty(t *testing.T) {
	got, err := parseJSONList("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"float64", float64(42), "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toString(tt.input); got != tt.want {
				t.Errorf("toString(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int64
	}{
		{"float64", float64(42), 42},
		{"int", int(10), 10},
		{"int64", int64(99), 99},
		{"nil", nil, 0},
		{"string", "nope", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toInt64(tt.input); got != tt.want {
				t.Errorf("toInt64(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTimeRFC3339(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{"standard RFC3339", "2024-06-15T10:30:00Z", 1718447400},
		{"with nanoseconds", "2024-06-15T10:30:00.123456789Z", 1718447400},
		{"empty", "", 0},
		{"invalid", "not-a-date", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTimeRFC3339(tt.input); got != tt.want {
				t.Errorf("parseTimeRFC3339(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestUnmarshalRepoTags(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int
	}{
		{"nil", nil, 0},
		{"slice of any", []any{"nginx:latest", "nginx:1.25"}, 2},
		{"string slice", []string{"alpine:3.19"}, 1},
		{"single string", "busybox:latest", 1},
		{"json string", `["a:1","b:2"]`, 2},
		{"empty string", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unmarshalRepoTags(tt.input)
			if len(got) != tt.want {
				t.Errorf("len = %d, want %d (got %v)", len(got), tt.want, got)
			}
		})
	}
}

func TestBaseCLI_ListImages(t *testing.T) {
	mock := &mockExecRunner{
		output: `[{"ID":"sha256:abc","RepoTags":["nginx:latest"],"Size":1000}]`,
	}
	withMockRunner(mock, func() {
		cli := baseCLI("docker")
		imgs, err := cli.listImages(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.gotCmd != "docker" {
			t.Errorf("cmd = %q, want docker", mock.gotCmd)
		}
		if len(mock.gotArgs) < 2 || mock.gotArgs[0] != "images" || mock.gotArgs[1] != "--format" {
			t.Errorf("args = %v, want [images --format json]", mock.gotArgs)
		}
		if len(imgs) != 1 {
			t.Fatalf("len = %d, want 1", len(imgs))
		}
		if imgs[0].ID != "sha256:abc" {
			t.Errorf("ID = %q, want sha256:abc", imgs[0].ID)
		}
	})
}

func TestBaseCLI_ListContainers(t *testing.T) {
	mock := &mockExecRunner{
		output: `[{"ID":"ctr1","ImageID":"sha256:abc"}]`,
	}
	withMockRunner(mock, func() {
		cli := baseCLI("podman")
		ctrs, err := cli.listContainers(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.gotCmd != "podman" {
			t.Errorf("cmd = %q, want podman", mock.gotCmd)
		}
		if len(ctrs) != 1 || ctrs[0].ImageID != "sha256:abc" {
			t.Errorf("unexpected containers: %v", ctrs)
		}
	})
}

func TestBaseCLI_InspectImage(t *testing.T) {
	mock := &mockExecRunner{
		output: `[{"Created":"2024-06-15T10:30:00Z","Config":{"Labels":{"app":"test"}},"RootFS":{"Layers":["sha256:l1"]}}]`,
	}
	withMockRunner(mock, func() {
		cli := baseCLI("docker")
		res, err := cli.inspectImage(context.Background(), "sha256:target")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ID != "sha256:target" {
			t.Errorf("ID = %q, want sha256:target", res.ID)
		}
		if len(res.Layers) != 1 || res.Layers[0] != "sha256:l1" {
			t.Errorf("Layers = %v, want [sha256:l1]", res.Layers)
		}
		if res.Labels["app"] != "test" {
			t.Errorf("Labels[app] = %q, want test", res.Labels["app"])
		}
	})
}

func TestBaseCLI_RemoveImage(t *testing.T) {
	mock := &mockExecRunner{output: "Untagged: nginx:latest\n"}
	withMockRunner(mock, func() {
		cli := baseCLI("docker")
		err := cli.removeImage(context.Background(), "sha256:abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.gotCmd != "docker" {
			t.Errorf("cmd = %q, want docker", mock.gotCmd)
		}
		if len(mock.gotArgs) != 2 || mock.gotArgs[0] != "rmi" || mock.gotArgs[1] != "sha256:abc" {
			t.Errorf("args = %v, want [rmi sha256:abc]", mock.gotArgs)
		}
	})
}

func TestBaseCLI_ListImages_CLIError(t *testing.T) {
	mock := &mockExecRunner{err: errors.New("docker not found")}
	withMockRunner(mock, func() {
		cli := baseCLI("docker")
		_, err := cli.listImages(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
