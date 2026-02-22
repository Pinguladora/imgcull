package runtime

import "context"

type PodmanAdapter struct {
	base baseCLI
}

func NewPodmanAdapter() *PodmanAdapter {
	return &PodmanAdapter{base: "podman"}
}

func (a *PodmanAdapter) ListImages(ctx context.Context) ([]Image, error) {
	return a.base.listImages(ctx)
}

func (a *PodmanAdapter) ListContainers(ctx context.Context) ([]Container, error) {
	return a.base.listContainers(ctx)
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
		return InspectResult{}, newEmptyInspectError(imageID)
	}
	return parsePodmanInspectResult(imageID, parsed[0])
}

func (a *PodmanAdapter) RemoveImage(ctx context.Context, imageID string) error {
	return a.base.removeImage(ctx, imageID)
}

func parsePodmanInspectResult(imageID string, obj map[string]any) (InspectResult, error) {
	created := parseTimeRFC3339(toString(obj["Created"]))
	labels := extractLabels(obj)
	layers := extractLayers(obj)
	layers = append(layers, extractGraphDriverLayers(obj)...)
	return InspectResult{
		ID:        imageID,
		CreatedAt: created,
		Labels:    labels,
		Layers:    layers,
	}, nil
}

func extractGraphDriverLayers(obj map[string]any) []string {
	var layers []string
	if gd, ok := obj["GraphDriver"].(map[string]any); ok {
		if data, ok := gd["Data"].(map[string]any); ok {
			if ll, ok := data["Layers"].([]any); ok {
				for _, v := range ll {
					layers = append(layers, toString(v))
				}
			}
		}
	}
	return layers
}

func newEmptyInspectError(imageID string) error {
	return &emptyInspectError{imageID: imageID}
}

type emptyInspectError struct {
	imageID string
}

func (e *emptyInspectError) Error() string {
	return "empty inspect for " + e.imageID
}
