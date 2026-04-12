package gc

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pinguladora/imgcull/internal/db"
	"github.com/pinguladora/imgcull/internal/runtime"
)

type integrationMockRunner struct {
	responses map[string]string
	removed   []string
}

func (m *integrationMockRunner) Run(_ context.Context, cmd string, args ...string) (string, error) {
	key := cmd + " " + strings.Join(args, " ")

	if len(args) >= 1 && args[0] == "rmi" {
		m.removed = append(m.removed, args[1])
		return "", nil
	}

	if len(args) >= 3 && args[0] == "image" && args[1] == "inspect" {
		inspectKey := cmd + " image inspect " + args[2]
		if resp, ok := m.responses[inspectKey]; ok {
			return resp, nil
		}
	}

	if resp, ok := m.responses[key]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func TestIntegration_ReconcileLayerAware(t *testing.T) {
	imgSize := int64(300_000_000)
	now := time.Now()

	// Give each unused image a distinct timestamp so sort order is deterministic.
	// img3 is oldest (72h), img4 is 60h, img5 is 48h.
	// After sorting by lastUsed ascending: img3, img4, img5.
	img3Time := now.Add(-72 * time.Hour)
	img4Time := now.Add(-60 * time.Hour)
	img5Time := now.Add(-48 * time.Hour)
	// Used images also get a timestamp; it won't matter since they're excluded.
	usedTime := now.Add(-24 * time.Hour)

	img3TimeStr := img3Time.UTC().Format(time.RFC3339)
	img4TimeStr := img4Time.UTC().Format(time.RFC3339)
	img5TimeStr := img5Time.UTC().Format(time.RFC3339)
	usedTimeStr := usedTime.UTC().Format(time.RFC3339)

	imagesJSON := fmt.Sprintf(`[
		{"ID":"img1","RepoTags":["nginx:latest"],"Size":%d},
		{"ID":"img2","RepoTags":["redis:7"],"Size":%d},
		{"ID":"img3","RepoTags":["app:v1"],"Size":%d},
		{"ID":"img4","RepoTags":["app:v2"],"Size":%d},
		{"ID":"img5","RepoTags":["old:stale"],"Size":%d}
	]`, imgSize, imgSize, imgSize, imgSize, imgSize)

	containersJSON := `[
		{"ID":"ctr1","ImageID":"img1"},
		{"ID":"ctr2","ImageID":"img2"}
	]`

	inspectTemplate := func(created string, layers []string) string {
		layerJSON := `[]`
		if len(layers) > 0 {
			parts := make([]string, len(layers))
			for i, l := range layers {
				parts[i] = fmt.Sprintf("%q", l)
			}
			layerJSON = "[" + strings.Join(parts, ",") + "]"
		}
		return fmt.Sprintf(`[{"Created":%q,"Config":{"Labels":{}},"RootFS":{"Type":"layers","Layers":%s}}]`, created, layerJSON)
	}

	mock := &integrationMockRunner{
		responses: map[string]string{
			"docker images --format json":      imagesJSON,
			"docker ps -a --format json":       containersJSON,
			"docker image inspect img1":        inspectTemplate(usedTimeStr, []string{"layerA", "layerB", "layerC"}),
			"docker image inspect img2":        inspectTemplate(usedTimeStr, []string{"layerA", "layerD"}),
			"docker image inspect img3":        inspectTemplate(img3TimeStr, []string{"layerA", "layerB", "layerE"}),
			"docker image inspect img4":        inspectTemplate(img4TimeStr, []string{"layerA", "layerF"}),
			"docker image inspect img5":        inspectTemplate(img5TimeStr, []string{"layerG", "layerH"}),
		},
	}

	old := runtime.Runner
	runtime.Runner = mock
	defer func() { runtime.Runner = old }()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	adapter := runtime.NewDockerAdapter()

	ctrl := NewController(
		ctx,
		adapter,
		database,
		500_000_000, // maxUnused: 500M
		60,          // poll interval (unused in test)
		"imgcull-keep",
		false, // not dry-run
		0,     // minAge: 0 hours (all qualify)
		10,    // chunk size
		0,     // no sleep between deletions
	)

	// Seed populates DB from runtime state
	if err := ctrl.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Verify seed stored all 5 images
	allMeta, err := database.GetAll()
	if err != nil {
		t.Fatalf("get all after seed: %v", err)
	}
	if len(allMeta) != 5 {
		t.Fatalf("seeded %d images, want 5", len(allMeta))
	}

	// Verify layers were stored
	if len(allMeta["img3"].Layers) != 3 {
		t.Errorf("img3 layers = %d, want 3", len(allMeta["img3"].Layers))
	}

	// Run one reconciliation cycle
	ctrl.reconcile(ctx)

	// Verify: with layer-aware sizing, only img3 should be deleted
	// img3 effective = 300M * 1/3 = 100M (layerA,B shared with used img1)
	// img4 effective = 300M * 1/2 = 150M (layerA shared with used img1,img2)
	// img5 effective = 300M * 2/2 = 300M (all unique)
	// Total effective unused = 100M + 150M + 300M = 550M
	// Over threshold (500M) by 50M
	//
	// Candidates sorted by lastUsed ascending: img3 (72h ago), img4 (60h ago), img5 (48h ago)
	// All 3 unused images have unique layers (layerE, layerF, layerG/H) so all are "leaves".
	// Partitioned order (leaves first, same relative order): img3, img4, img5
	//
	// Delete img3 (effective 100M): 550M - 100M = 450M <= 500M. Stop.
	if len(mock.removed) != 1 {
		t.Fatalf("removed %d images, want 1. Removed: %v", len(mock.removed), mock.removed)
	}
	if mock.removed[0] != "img3" {
		t.Errorf("removed %q, want img3", mock.removed[0])
	}

	// Verify DB updated: img3 removed
	allMeta, err = database.GetAll()
	if err != nil {
		t.Fatalf("get all after reconcile: %v", err)
	}
	if _, ok := allMeta["img3"]; ok {
		t.Error("img3 should be removed from DB after deletion")
	}
	if len(allMeta) != 4 {
		t.Errorf("remaining images = %d, want 4", len(allMeta))
	}
}
