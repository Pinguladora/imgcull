package runtime

import "context"

type DockerAdapter struct {
	base baseCLI
}

func NewDockerAdapter() *DockerAdapter {
	return &DockerAdapter{base: "docker"}
}

func (a *DockerAdapter) ListImages(ctx context.Context) ([]Image, error) {
	return a.base.listImages(ctx)
}

func (a *DockerAdapter) ListContainers(ctx context.Context) ([]Container, error) {
	return a.base.listContainers(ctx)
}

func (a *DockerAdapter) InspectImage(ctx context.Context, imageID string) (InspectResult, error) {
	return a.base.inspectImage(ctx, imageID)
}

func (a *DockerAdapter) RemoveImage(ctx context.Context, imageID string) error {
	return a.base.removeImage(ctx, imageID)
}
