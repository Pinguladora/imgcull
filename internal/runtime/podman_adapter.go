package runtime

import (
	"context"
	"fmt"
)

// PodmanAdapter is a thin wrapper around the podman CLI.
type PodmanAdapter struct{}

// NewPodmanAdapter constructs a PodmanAdapter.
func NewPodmanAdapter() *PodmanAdapter { return &PodmanAdapter{} }

func (a *PodmanAdapter) ListImages(ctx context.Context) ([]Image, error) {
	out, err := runCLI(ctx, "podman", "images", "--format", "json")
	if err != nil {
		return nil, err
	}
	parsed, err := parseJSONList(out)
	if err != nil {
		return nil, err
	}
	res := make([]Image, 0, len(parsed))
	for _, m := range parsed {
		id := toString(m["Id"])
		if id == "" {
			id = toString(m["ID"])
		}
		tags := unmarshalRepoTags(m["RepoTags"])
		size := toInt64(m["Size"])
		res = append(res, Image{ID: id, RepoTags: tags, Size: size})
	}
	return res, nil
}

func (a *PodmanAdapter) ListContainers(ctx context.Context) ([]Container, error) {
	out, err := runCLI(ctx, "podman", "ps", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	parsed, err := parseJSONList(out)
	if err != nil {
		return nil, err
	}
	res := make([]Container, 0, len(parsed))
	for _, m := range parsed {
		id := toString(m["Id"])
		if id == "" {
			id = toString(m["ID"])
		}
		img := toString(m["ImageID"])
		if img == "" {
			img = toString(m["Image"])
		}
		res = append(res, Container{ID: id, ImageID: img})
	}
	return res, nil
}

func (a *PodmanAdapter) InspectImage(ctx context.Context, imageID string) (InspectResult, error) {
	out, err := runCLI(ctx, "podman", "image", "inspect", imageID)
	if err != nil {
		return InspectResult{}, err
	}
	parsed, err := parseJSONList(out)
	if err != nil {
		return InspectResult{}, err
	}
	if len(parsed) == 0 {
		return InspectResult{}, fmt.Errorf("empty inspect for %s", imageID)
	}
	obj := parsed[0]

	created := parseTimeRFC3339(toString(obj["Created"]))
	labels := map[string]string{}

	// Config.Labels
	if cfg, ok := obj["Config"].(map[string]any); ok {
		if ll, ok := cfg["Labels"].(map[string]any); ok {
			for k, v := range ll {
				labels[k] = toString(v)
			}
		}
	}
	// top-level Labels (some versions)
	if ll, ok := obj["Labels"].(map[string]any); ok {
		for k, v := range ll {
			labels[k] = toString(v)
		}
	}

	// Layer IDs from RootFS.Layers or GraphDriver.Data.Layers
	layers := []string{}
	if rf, ok := obj["RootFS"].(map[string]any); ok {
		if ll, ok := rf["Layers"].([]any); ok {
			for _, v := range ll {
				layers = append(layers, toString(v))
			}
		}
	}
	if gd, ok := obj["GraphDriver"].(map[string]any); ok {
		if data, ok := gd["Data"].(map[string]any); ok {
			if ll, ok := data["Layers"].([]any); ok {
				for _, v := range ll {
					layers = append(layers, toString(v))
				}
			}
		}
	}

	return InspectResult{
		ID:        imageID,
		CreatedAt: created,
		Labels:    labels,
		Layers:    layers,
	}, nil
}

func (a *PodmanAdapter) RemoveImage(ctx context.Context, imageID string) error {
	_, err := runCLI(ctx, "podman", "rmi", imageID)
	return err
}
