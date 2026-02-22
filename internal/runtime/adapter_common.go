package runtime

import (
	"context"
	"fmt"
)

type baseCLI string

func (b baseCLI) listImages(ctx context.Context) ([]Image, error) {
	out, err := runCLI(ctx, string(b), "images", "--format", "json")
	if err != nil {
		return nil, err
	}
	parsed, err := parseJSONList(out)
	if err != nil {
		return nil, err
	}
	return parseImages(parsed), nil
}

func (b baseCLI) listContainers(ctx context.Context) ([]Container, error) {
	out, err := runCLI(ctx, string(b), "ps", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	parsed, err := parseJSONList(out)
	if err != nil {
		return nil, err
	}
	return parseContainers(parsed), nil
}

func (b baseCLI) inspectImage(ctx context.Context, imageID string) (InspectResult, error) {
	out, err := runCLI(ctx, string(b), "image", "inspect", imageID)
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
	return parseInspectResult(imageID, parsed[0])
}

func (b baseCLI) removeImage(ctx context.Context, imageID string) error {
	_, err := runCLI(ctx, string(b), "rmi", imageID)
	return err
}

func parseImages(parsed []map[string]any) []Image {
	res := make([]Image, 0, len(parsed))
	for _, m := range parsed {
		id := toString(m["Id"])
		if id == "" {
			id = toString(m["ID"])
		}
		res = append(res, Image{
			ID:       id,
			RepoTags: unmarshalRepoTags(m["RepoTags"]),
			Size:     toInt64(m["Size"]),
		})
	}
	return res
}

func parseContainers(parsed []map[string]any) []Container {
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
	return res
}

func parseInspectResult(imageID string, obj map[string]any) (InspectResult, error) {
	created := parseTimeRFC3339(toString(obj["Created"]))
	labels := extractLabels(obj)
	layers := extractLayers(obj)
	return InspectResult{
		ID:        imageID,
		CreatedAt: created,
		Labels:    labels,
		Layers:    layers,
	}, nil
}

func extractLabels(obj map[string]any) map[string]string {
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
	return labels
}

func extractLayers(obj map[string]any) []string {
	var layers []string
	if rf, ok := obj["RootFS"].(map[string]any); ok {
		if ll, ok := rf["Layers"].([]any); ok {
			for _, v := range ll {
				layers = append(layers, toString(v))
			}
		}
	}
	return layers
}
