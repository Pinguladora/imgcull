package runtime

import (
	"context"
	"fmt"
)

// NerdctlAdapter is a wrapper around the `nerdctl` CLI (containerd).
type NerdctlAdapter struct{}

// NewNerdctlAdapter constructs a NerdctlAdapter.
func NewNerdctlAdapter() *NerdctlAdapter { return &NerdctlAdapter{} }

// ListImages returns all images from the Nerdctl runtime.
func (a *NerdctlAdapter) ListImages(ctx context.Context) ([]Image, error) {
	out, err := runCLI(ctx, "nerdctl", "images", "--format", "json")
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

// ListContainers returns all containers (including stopped ones) from Nerdctl runtime.
func (a *NerdctlAdapter) ListContainers(ctx context.Context) ([]Container, error) {
	out, err := runCLI(ctx, "nerdctl", "ps", "-a", "--format", "json")
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

// InspectImage returns detailed info about the given image ID from Nerdctl runtime.
func (a *NerdctlAdapter) InspectImage(ctx context.Context, imageID string) (InspectResult, error) {
	out, err := runCLI(ctx, "nerdctl", "image", "inspect", imageID)
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
	if cfg, ok := obj["Config"].(map[string]any); ok {
		if ll, ok := cfg["Labels"].(map[string]any); ok {
			for k, v := range ll {
				labels[k] = toString(v)
			}
		}
	}
	if ll, ok := obj["Labels"].(map[string]any); ok {
		for k, v := range ll {
			labels[k] = toString(v)
		}
	}

	layers := []string{}
	if rf, ok := obj["RootFS"].(map[string]any); ok {
		if ll, ok := rf["Layers"].([]any); ok {
			for _, v := range ll {
				layers = append(layers, toString(v))
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

// RemoveImage removes the image with the given ID from the Nerdctl runtime.
func (a *NerdctlAdapter) RemoveImage(ctx context.Context, imageID string) error {
	_, err := runCLI(ctx, "nerdctl", "rmi", imageID)
	return err
}
