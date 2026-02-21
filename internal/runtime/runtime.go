package runtime

import "context"

// Image holds the minimal information GC needs about an image.
type Image struct {
	ID       string
	RepoTags []string
	Size     int64
}

// Container holds minimal information to know which images are in-use.
type Container struct {
	ID      string
	ImageID string
}

// InspectResult includes created timestamp, labels and layers (when available).
type InspectResult struct {
	ID        string
	CreatedAt int64
	Labels    map[string]string
	Layers    []string
}

// Adapter is the runtime abstraction.
type Adapter interface {
	ListImages(ctx context.Context) ([]Image, error)
	ListContainers(ctx context.Context) ([]Container, error)
	InspectImage(ctx context.Context, imageID string) (InspectResult, error)
	RemoveImage(ctx context.Context, imageID string) error
}
