package runtime

import "testing"

func TestParseImages(t *testing.T) {
	tests := []struct {
		name  string
		input []map[string]any
		want  []Image
	}{
		{
			name: "docker format with ID uppercase",
			input: []map[string]any{
				{
					"ID":       "sha256:abc123",
					"RepoTags": []any{"nginx:latest", "nginx:1.25"},
					"Size":     float64(187654321),
				},
			},
			want: []Image{
				{ID: "sha256:abc123", RepoTags: []string{"nginx:latest", "nginx:1.25"}, Size: 187654321},
			},
		},
		{
			name: "podman format with Id lowercase",
			input: []map[string]any{
				{
					"Id":       "def456",
					"RepoTags": []any{"alpine:3.19"},
					"Size":     float64(7800000),
				},
			},
			want: []Image{
				{ID: "def456", RepoTags: []string{"alpine:3.19"}, Size: 7800000},
			},
		},
		{
			name: "no repo tags",
			input: []map[string]any{
				{
					"ID":   "sha256:orphan1",
					"Size": float64(5000),
				},
			},
			want: []Image{
				{ID: "sha256:orphan1", RepoTags: nil, Size: 5000},
			},
		},
		{
			name:  "empty input",
			input: nil,
			want:  []Image{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseImages(tt.input)
			if len(tt.input) == 0 && len(got) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID {
					t.Errorf("[%d] ID = %q, want %q", i, got[i].ID, tt.want[i].ID)
				}
				if got[i].Size != tt.want[i].Size {
					t.Errorf("[%d] Size = %d, want %d", i, got[i].Size, tt.want[i].Size)
				}
				if len(got[i].RepoTags) != len(tt.want[i].RepoTags) {
					t.Errorf("[%d] RepoTags len = %d, want %d", i, len(got[i].RepoTags), len(tt.want[i].RepoTags))
				}
			}
		})
	}
}

func TestParseContainers(t *testing.T) {
	tests := []struct {
		name  string
		input []map[string]any
		want  []Container
	}{
		{
			name: "docker format with ImageID",
			input: []map[string]any{
				{"ID": "ctr1", "ImageID": "sha256:abc123"},
			},
			want: []Container{
				{ID: "ctr1", ImageID: "sha256:abc123"},
			},
		},
		{
			name: "podman format with Id and Image",
			input: []map[string]any{
				{"Id": "ctr2", "Image": "sha256:def456"},
			},
			want: []Container{
				{ID: "ctr2", ImageID: "sha256:def456"},
			},
		},
		{
			name:  "empty input",
			input: nil,
			want:  []Container{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseContainers(tt.input)
			if len(tt.input) == 0 && len(got) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID {
					t.Errorf("[%d] ID = %q, want %q", i, got[i].ID, tt.want[i].ID)
				}
				if got[i].ImageID != tt.want[i].ImageID {
					t.Errorf("[%d] ImageID = %q, want %q", i, got[i].ImageID, tt.want[i].ImageID)
				}
			}
		})
	}
}

func TestParseInspectResult(t *testing.T) {
	tests := []struct {
		name    string
		imageID string
		input   map[string]any
		want    InspectResult
	}{
		{
			name:    "docker format with Config.Labels and RootFS.Layers",
			imageID: "sha256:abc123",
			input: map[string]any{
				"Created": "2024-06-15T10:30:00Z",
				"Config": map[string]any{
					"Labels": map[string]any{
						"maintainer": "test",
						"version":    "1.0",
					},
				},
				"RootFS": map[string]any{
					"Type":   "layers",
					"Layers": []any{"sha256:layer1", "sha256:layer2", "sha256:layer3"},
				},
			},
			want: InspectResult{
				ID:        "sha256:abc123",
				CreatedAt: 1718447400,
				Labels:    map[string]string{"maintainer": "test", "version": "1.0"},
				Layers:    []string{"sha256:layer1", "sha256:layer2", "sha256:layer3"},
			},
		},
		{
			name:    "podman format with top-level Labels",
			imageID: "def456",
			input: map[string]any{
				"Created": "2024-06-15T10:30:00Z",
				"Labels": map[string]any{
					"app": "myapp",
				},
				"RootFS": map[string]any{
					"Layers": []any{"sha256:layerA"},
				},
			},
			want: InspectResult{
				ID:        "def456",
				CreatedAt: 1718447400,
				Labels:    map[string]string{"app": "myapp"},
				Layers:    []string{"sha256:layerA"},
			},
		},
		{
			name:    "no layers or labels",
			imageID: "nolayers",
			input: map[string]any{
				"Created": "2024-06-15T10:30:00Z",
			},
			want: InspectResult{
				ID:        "nolayers",
				CreatedAt: 1718447400,
				Labels:    map[string]string{},
				Layers:    nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInspectResult(tt.imageID, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != tt.want.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.want.ID)
			}
			if got.CreatedAt != tt.want.CreatedAt {
				t.Errorf("CreatedAt = %d, want %d", got.CreatedAt, tt.want.CreatedAt)
			}
			if len(got.Labels) != len(tt.want.Labels) {
				t.Errorf("Labels len = %d, want %d", len(got.Labels), len(tt.want.Labels))
			}
			for k, v := range tt.want.Labels {
				if got.Labels[k] != v {
					t.Errorf("Labels[%q] = %q, want %q", k, got.Labels[k], v)
				}
			}
			if len(got.Layers) != len(tt.want.Layers) {
				t.Fatalf("Layers len = %d, want %d", len(got.Layers), len(tt.want.Layers))
			}
			for i := range got.Layers {
				if got.Layers[i] != tt.want.Layers[i] {
					t.Errorf("Layers[%d] = %q, want %q", i, got.Layers[i], tt.want.Layers[i])
				}
			}
		})
	}
}
