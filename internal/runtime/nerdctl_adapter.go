package runtime

import "context"

type NerdctlAdapter struct {
	base baseCLI
}

func NewNerdctlAdapter() *NerdctlAdapter {
	return &NerdctlAdapter{base: "nerdctl"}
}

func (a *NerdctlAdapter) ListImages(ctx context.Context) ([]Image, error) {
	return a.base.listImages(ctx)
}

func (a *NerdctlAdapter) ListContainers(ctx context.Context) ([]Container, error) {
	return a.base.listContainers(ctx)
}

func (a *NerdctlAdapter) InspectImage(ctx context.Context, imageID string) (InspectResult, error) {
	return a.base.inspectImage(ctx, imageID)
}

func (a *NerdctlAdapter) RemoveImage(ctx context.Context, imageID string) error {
	return a.base.removeImage(ctx, imageID)
}
