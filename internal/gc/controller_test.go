package gc

import (
	"context"
	"testing"

	"github.com/pinguladora/imgcull/internal/db"
	"github.com/pinguladora/imgcull/internal/runtime"
)

type mockAdapter struct {
	images     []runtime.Image
	containers []runtime.Container
	inspects   map[string]runtime.InspectResult
	removed    []string
	removeErr  error
}

func (m *mockAdapter) ListImages(_ context.Context) ([]runtime.Image, error) {
	return m.images, nil
}

func (m *mockAdapter) ListContainers(_ context.Context) ([]runtime.Container, error) {
	return m.containers, nil
}

func (m *mockAdapter) InspectImage(_ context.Context, id string) (runtime.InspectResult, error) {
	if r, ok := m.inspects[id]; ok {
		return r, nil
	}
	return runtime.InspectResult{}, nil
}

func (m *mockAdapter) RemoveImage(_ context.Context, id string) error {
	if m.removeErr != nil {
		return m.removeErr
	}
	m.removed = append(m.removed, id)
	return nil
}

func TestBuildUsedSet(t *testing.T) {
	containers := []runtime.Container{
		{ID: "ctr1", ImageID: "img1"},
		{ID: "ctr2", ImageID: "img2"},
		{ID: "ctr3", ImageID: ""},
		{ID: "ctr4", ImageID: "img1"},
	}
	used := buildUsedSet(containers)
	if len(used) != 2 {
		t.Fatalf("len = %d, want 2", len(used))
	}
	if _, ok := used["img1"]; !ok {
		t.Error("img1 should be in used set")
	}
	if _, ok := used["img2"]; !ok {
		t.Error("img2 should be in used set")
	}
	if _, ok := used[""]; ok {
		t.Error("empty string should not be in used set")
	}
}

func TestBuildCandidates(t *testing.T) {
	allMeta := map[string]db.ImageMeta{
		"img1": {DisplayName: "nginx:latest", Size: 1000, LastUsedTs: 100, Labels: `{}`},
		"img2": {DisplayName: "alpine:3.19", Size: 500, LastUsedTs: 200, Labels: `{"imgcull-keep":"true"}`},
		"img3": {DisplayName: "busybox:latest", Size: 300, LastUsedTs: 50, Labels: `{}`},
	}
	used := map[string]struct{}{"img1": {}}

	ctrl := &Controller{keepLabel: "imgcull-keep"}
	cands := ctrl.buildCandidates(allMeta, used)

	if len(cands) != 1 {
		t.Fatalf("len = %d, want 1 (img3 only: img1 in-use, img2 has keep label)", len(cands))
	}
	if cands[0].id != "img3" {
		t.Errorf("candidate id = %q, want img3", cands[0].id)
	}
	if cands[0].size != 300 {
		t.Errorf("candidate size = %d, want 300", cands[0].size)
	}
}

func TestBuildCandidates_NoKeepLabel(t *testing.T) {
	allMeta := map[string]db.ImageMeta{
		"img1": {DisplayName: "a", Size: 100, Labels: `{}`},
		"img2": {DisplayName: "b", Size: 200, Labels: `{}`},
	}
	used := map[string]struct{}{}

	ctrl := &Controller{keepLabel: "imgcull-keep"}
	cands := ctrl.buildCandidates(allMeta, used)

	if len(cands) != 2 {
		t.Fatalf("len = %d, want 2", len(cands))
	}
}

func TestBuildLayerMaps(t *testing.T) {
	allMeta := map[string]db.ImageMeta{
		"img1": {Layers: []string{"layerA", "layerB"}},
		"img2": {Layers: []string{"layerA", "layerC"}},
		"img3": {Layers: nil},
	}

	layerRef, imgLayers := buildLayerMaps(allMeta)

	if layerRef["layerA"] != 2 {
		t.Errorf("layerA refcount = %d, want 2", layerRef["layerA"])
	}
	if layerRef["layerB"] != 1 {
		t.Errorf("layerB refcount = %d, want 1", layerRef["layerB"])
	}
	if layerRef["layerC"] != 1 {
		t.Errorf("layerC refcount = %d, want 1", layerRef["layerC"])
	}
	if len(imgLayers) != 2 {
		t.Errorf("imgLayers len = %d, want 2 (img3 has no layers)", len(imgLayers))
	}
}

func TestPartitionByUniqueLayers(t *testing.T) {
	cands := []candidate{
		{id: "shared-only", last: 100},
		{id: "has-unique", last: 200},
	}
	imgLayers := map[string][]string{
		"shared-only": {"layerA"},
		"has-unique":  {"layerA", "layerB"},
	}
	layerRef := map[string]int{
		"layerA": 5,
		"layerB": 1,
	}

	ordered := partitionByUniqueLayers(cands, imgLayers, layerRef)

	if len(ordered) != 2 {
		t.Fatalf("len = %d, want 2", len(ordered))
	}
	if ordered[0].id != "has-unique" {
		t.Errorf("first candidate = %q, want has-unique (has unique layerB)", ordered[0].id)
	}
	if ordered[1].id != "shared-only" {
		t.Errorf("second candidate = %q, want shared-only", ordered[1].id)
	}
}

func TestHasKeepLabel(t *testing.T) {
	tests := []struct {
		name   string
		labels string
		keep   string
		want   bool
	}{
		{"label present", `{"imgcull-keep":"true"}`, "imgcull-keep", true},
		{"label absent", `{"other":"value"}`, "imgcull-keep", false},
		{"empty labels", `{}`, "imgcull-keep", false},
		{"empty labels string", "", "imgcull-keep", false},
		{"empty keep", `{"imgcull-keep":"true"}`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasKeepLabel(tt.labels, tt.keep); got != tt.want {
				t.Errorf("hasKeepLabel(%q, %q) = %v, want %v", tt.labels, tt.keep, got, tt.want)
			}
		})
	}
}

func TestComputeUnused_Fallback_NoLayers(t *testing.T) {
	ctrl := &Controller{}
	imgs := []runtime.Image{
		{ID: "used1", Size: 500},
		{ID: "unused1", Size: 300},
		{ID: "unused2", Size: 200},
	}
	used := map[string]struct{}{"used1": {}}
	allMeta := map[string]db.ImageMeta{
		"used1":   {Size: 500},
		"unused1": {Size: 300},
		"unused2": {Size: 200},
	}

	rawSizes, effectiveSizes, unusedTotal := ctrl.computeUnused(imgs, used, allMeta)

	if rawSizes["unused1"] != 300 {
		t.Errorf("rawSizes[unused1] = %d, want 300", rawSizes["unused1"])
	}
	if effectiveSizes["unused1"] != 300 {
		t.Errorf("effectiveSizes[unused1] = %d, want 300 (no layers = fallback)", effectiveSizes["unused1"])
	}
	if unusedTotal != 500 {
		t.Errorf("unusedTotal = %d, want 500", unusedTotal)
	}
}

func TestComputeUnused_LayerAware(t *testing.T) {
	ctrl := &Controller{}

	imgs := []runtime.Image{
		{ID: "used1", Size: 300_000_000},
		{ID: "unused1", Size: 300_000_000},
		{ID: "unused2", Size: 300_000_000},
	}
	used := map[string]struct{}{"used1": {}}

	allMeta := map[string]db.ImageMeta{
		"used1": {
			Size:   300_000_000,
			Layers: []string{"layerA", "layerB", "layerC"},
		},
		"unused1": {
			Size:   300_000_000,
			Layers: []string{"layerA", "layerB", "layerD"},
		},
		"unused2": {
			Size:   300_000_000,
			Layers: []string{"layerE", "layerF"},
		},
	}

	rawSizes, effectiveSizes, unusedTotal := ctrl.computeUnused(imgs, used, allMeta)

	// unused1: 3 layers, layerA and layerB shared with used1, layerD unique
	// uniqueRatio = 1/3, effectiveSize = 300M * 1/3 = 100M
	wantEff1 := int64(float64(300_000_000) * 1.0 / 3.0)
	if effectiveSizes["unused1"] != wantEff1 {
		t.Errorf("effectiveSizes[unused1] = %d, want %d", effectiveSizes["unused1"], wantEff1)
	}

	// unused2: 2 layers, both unique (not shared with any used image)
	// uniqueRatio = 2/2 = 1.0, effectiveSize = 300M
	if effectiveSizes["unused2"] != 300_000_000 {
		t.Errorf("effectiveSizes[unused2] = %d, want 300000000", effectiveSizes["unused2"])
	}

	// Raw sizes unchanged
	if rawSizes["unused1"] != 300_000_000 {
		t.Errorf("rawSizes[unused1] = %d, want 300000000", rawSizes["unused1"])
	}

	// unusedTotal = 100M + 300M = 400M
	wantTotal := wantEff1 + 300_000_000
	if unusedTotal != wantTotal {
		t.Errorf("unusedTotal = %d, want %d", unusedTotal, wantTotal)
	}
}

func TestComputeUnused_MixedLayersAndNoLayers(t *testing.T) {
	ctrl := &Controller{}

	imgs := []runtime.Image{
		{ID: "used1", Size: 100},
		{ID: "unused1", Size: 400},
		{ID: "unused2", Size: 200},
	}
	used := map[string]struct{}{"used1": {}}

	allMeta := map[string]db.ImageMeta{
		"used1": {
			Size:   100,
			Layers: []string{"layerA"},
		},
		"unused1": {
			Size:   400,
			Layers: []string{"layerA", "layerB"},
		},
		"unused2": {
			Size: 200,
			// No layers: runtime didn't return them
		},
	}

	_, effectiveSizes, unusedTotal := ctrl.computeUnused(imgs, used, allMeta)

	// unused1: 2 layers, layerA shared, layerB unique. ratio=1/2. effective=200
	if effectiveSizes["unused1"] != 200 {
		t.Errorf("effectiveSizes[unused1] = %d, want 200", effectiveSizes["unused1"])
	}
	// unused2: no layers, fallback to full size
	if effectiveSizes["unused2"] != 200 {
		t.Errorf("effectiveSizes[unused2] = %d, want 200 (fallback)", effectiveSizes["unused2"])
	}
	// total = 200 + 200 = 400
	if unusedTotal != 400 {
		t.Errorf("unusedTotal = %d, want 400", unusedTotal)
	}
}
