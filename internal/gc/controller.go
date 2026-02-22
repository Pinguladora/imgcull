package gc

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/pinguladora/imgcull/internal/db"
	"github.com/pinguladora/imgcull/internal/runtime"
)

type candidate struct {
	id      string
	display string
	labels  string
	size    int64
	last    int64
}

type Controller struct {
	runtime         runtime.Adapter
	db              *db.DB
	keepLabel       string
	maxUnused       int64
	pollInterval    time.Duration
	minAge          time.Duration
	deletionChunk   int
	deletionSleepMs int
	dry             bool
}

func NewController(
	ctx context.Context,
	rt runtime.Adapter,
	database *db.DB,
	maxUnused int64,
	pollSec int,
	keepLabel string,
	dry bool,
	minAgeHours int,
	chunk int,
	sleep int,
) *Controller {
	return &Controller{
		runtime:         rt,
		db:              database,
		maxUnused:       maxUnused,
		pollInterval:    time.Duration(pollSec) * time.Second,
		keepLabel:       keepLabel,
		dry:             dry,
		minAge:          time.Duration(minAgeHours) * time.Hour,
		deletionChunk:   chunk,
		deletionSleepMs: sleep,
	}
}

func (c *Controller) Seed(ctx context.Context) error {
	log.Info().Msg("seeding DB from runtime images")
	imgs, err := c.runtime.ListImages(ctx)
	if err != nil {
		log.Error().Err(err).Msg("list images failed")
		return fmt.Errorf("list images: %w", err)
	}
	containers, _ := c.runtime.ListContainers(ctx)
	used := map[string]struct{}{}
	for _, ct := range containers {
		if ct.ImageID != "" {
			used[ct.ImageID] = struct{}{}
		}
	}

	for _, img := range imgs {
		ins, _ := c.runtime.InspectImage(ctx, img.ID)
		display := "<none>"
		if len(img.RepoTags) > 0 && img.RepoTags[0] != "" {
			display = img.RepoTags[0]
		}
		lastUsed := int64(0)
		if _, ok := used[img.ID]; ok {
			lastUsed = time.Now().Unix()
		} else if ins.CreatedAt != 0 {
			lastUsed = ins.CreatedAt
		}
		labelsB, _ := json.Marshal(ins.Labels)
		meta := db.ImageMeta{
			RepoTags:    mustMarshalString(img.RepoTags),
			DisplayName: display,
			Size:        img.Size,
			CreatedTs:   ins.CreatedAt,
			LastUsedTs:  lastUsed,
			Labels:      string(labelsB),
			Layers:      ins.Layers,
		}
		_ = c.db.Upsert(img.ID, meta)
	}
	log.Info().Msg("seed complete")
	return nil
}

func (c *Controller) RunLoop(ctx context.Context) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.reconcile(ctx)
		}
	}
}

func (c *Controller) reconcile(ctx context.Context) {
	log.Info().Msg("reconcile start")

	imgs, containers, err := c.fetchRuntimeState(ctx)
	if err != nil {
		return
	}
	used := buildUsedSet(containers)

	imageSizes, unusedTotal := c.computeUnused(imgs, used)
	log.Info().Int64("unused_bytes", unusedTotal).Msg("unused computed")
	if unusedTotal <= c.maxUnused {
		return
	}

	allMeta, err := c.db.GetAll()
	if err != nil {
		log.Error().Err(err).Msg("db read failed")
		return
	}

	layerRef, imgLayers := buildLayerMaps(allMeta)
	cands := c.buildCandidates(allMeta, used)
	sort.Slice(cands, func(i, j int) bool { return cands[i].last < cands[j].last })

	ordered := partitionByUniqueLayers(cands, imgLayers, layerRef)

	c.performDeletions(ctx, ordered, imageSizes, allMeta, unusedTotal)

	log.Info().Msg("reconcile finished")
}

func (c *Controller) fetchRuntimeState(ctx context.Context) ([]runtime.Image, []runtime.Container, error) {
	imgs, err := c.runtime.ListImages(ctx)
	if err != nil {
		log.Error().Err(err).Msg("list images failed")
		return nil, nil, fmt.Errorf("list images: %w", err)
	}
	containers, _ := c.runtime.ListContainers(ctx)
	return imgs, containers, nil
}

func buildUsedSet(containers []runtime.Container) map[string]struct{} {
	used := map[string]struct{}{}
	for _, ct := range containers {
		if ct.ImageID != "" {
			used[ct.ImageID] = struct{}{}
		}
	}
	return used
}

func (c *Controller) computeUnused(imgs []runtime.Image, used map[string]struct{}) (map[string]int64, int64) {
	imageSizes := map[string]int64{}
	unusedTotal := int64(0)
	for _, img := range imgs {
		imageSizes[img.ID] = img.Size
		if _, ok := used[img.ID]; !ok {
			unusedTotal += img.Size
		}
	}
	return imageSizes, unusedTotal
}

func buildLayerMaps(allMeta map[string]db.ImageMeta) (map[string]int, map[string][]string) {
	layerRef := map[string]int{}
	imgLayers := map[string][]string{}
	for id, m := range allMeta {
		if len(m.Layers) == 0 {
			continue
		}
		imgLayers[id] = m.Layers
		for _, l := range m.Layers {
			layerRef[l]++
		}
	}
	return layerRef, imgLayers
}

func (c *Controller) buildCandidates(allMeta map[string]db.ImageMeta, used map[string]struct{}) []candidate {
	var cands []candidate
	for id, m := range allMeta {
		if _, inUse := used[id]; inUse {
			continue
		}
		if c.keepLabel != "" && hasKeepLabel(m.Labels, c.keepLabel) {
			continue
		}
		cands = append(cands, candidate{
			id:      id,
			size:    m.Size,
			last:    m.LastUsedTs,
			display: m.DisplayName,
			labels:  m.Labels,
		})
	}
	return cands
}

func partitionByUniqueLayers(cands []candidate, imgLayers map[string][]string, layerRef map[string]int) []candidate {
	var leaves, nonLeaves []candidate
	for _, x := range cands {
		hasUnique := hasUniqueLayer(imgLayers[x.id], layerRef)
		if hasUnique {
			leaves = append(leaves, x)
		} else {
			nonLeaves = append(nonLeaves, x)
		}
	}
	return slices.Concat(leaves, nonLeaves)
}

func hasUniqueLayer(layers []string, layerRef map[string]int) bool {
	for _, l := range layers {
		if layerRef[l] == 1 {
			return true
		}
	}
	return false
}

func (c *Controller) performDeletions(
	ctx context.Context,
	ordered []candidate,
	imageSizes map[string]int64,
	allMeta map[string]db.ImageMeta,
	unusedTotal int64,
) {
	deleted := 0
	freed := int64(0)
	for _, x := range ordered {
		if deleted >= c.deletionChunk {
			log.Info().Int("deleted", deleted).Msg("chunk limit reached")
			break
		}
		sz, ok := imageSizes[x.id]
		if !ok {
			_ = c.db.Remove(x.id)
			continue
		}
		if m, ok := allMeta[x.id]; ok && m.CreatedTs != 0 {
			if time.Since(time.Unix(m.CreatedTs, 0)) < c.minAge {
				log.Info().Str("image", x.display).Msg("skip: min-age")
				continue
			}
		}
		if c.dry {
			c.logDryRun(x.id, x.display, sz)
			deleted++
			freed += sz
		} else if c.deleteImage(ctx, x.id, x.display, sz) {
			deleted++
			freed += sz
		}
		if unusedTotal-freed <= c.maxUnused {
			break
		}
	}
	log.Info().Int("deleted_count", deleted).Int64("freed_bytes", freed).Msg("deletions complete")
}

func (c *Controller) logDryRun(id, display string, sz int64) {
	log.Info().
		Str("op", "dry-remove").
		Str("image_id", id).
		Str("display", display).
		Int64("size", sz).
		Msg("DRY-RUN candidate")
}

func (c *Controller) deleteImage(ctx context.Context, id, display string, sz int64) bool {
	log.Info().
		Str("op", "remove").
		Str("image_id", id).
		Str("display", display).
		Int64("size", sz).
		Msg("removing image")
	if err := c.runtime.RemoveImage(ctx, id); err != nil {
		log.Error().Err(err).Str("image", display).Msg("remove failed")
		return false
	}
	_ = c.db.Remove(id)
	if c.deletionSleepMs > 0 {
		time.Sleep(time.Duration(c.deletionSleepMs) * time.Millisecond)
	}
	return true
}

func mustMarshalString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func hasKeepLabel(labels string, keep string) bool {
	if keep == "" || labels == "" {
		return false
	}
	var m map[string]string
	_ = json.Unmarshal([]byte(labels), &m)
	_, ok := m[keep]
	return ok
}
