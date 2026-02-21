package gc

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/pinguladora/imgcull/internal/db"
	"github.com/pinguladora/imgcull/internal/runtime"
	"github.com/rs/zerolog/log"
)

// Controller implements the GC loop and uses a runtime.Adapter and a db.DB.
type Controller struct {
	runtime         runtime.Adapter
	ctx             context.Context
	db              *db.DB
	keepLabel       string
	maxUnused       int64
	pollInterval    time.Duration
	minAge          time.Duration
	deletionChunk   int
	deletionSleepMs int
	dry             bool
}

// NewController constructs a Controller.
func NewController(ctx context.Context, rt runtime.Adapter, database *db.DB, maxUnused int64, pollSec int, keepLabel string, dry bool, minAgeHours int, chunk int, sleep int) *Controller {
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
		ctx:             ctx,
	}
}

// Seed populates the DB from the runtime's current images and containers.
func (c *Controller) Seed() error {
	log.Info().Msg("seeding DB from runtime images")
	imgs, err := c.runtime.ListImages(c.ctx)
	if err != nil {
		return err
	}
	containers, _ := c.runtime.ListContainers(c.ctx)
	used := map[string]struct{}{}
	for _, ct := range containers {
		if ct.ImageID != "" {
			used[ct.ImageID] = struct{}{}
		}
	}

	for _, img := range imgs {
		ins, _ := c.runtime.InspectImage(c.ctx, img.ID)
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

// RunLoop starts the reconcile ticker and blocks until context cancellation.
func (c *Controller) RunLoop() {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.reconcile()
		}
	}
}

// reconcile performs one GC reconciliation pass.
func (c *Controller) reconcile() {
	log.Info().Msg("reconcile start")

	// get runtime state
	imgs, err := c.runtime.ListImages(c.ctx)
	if err != nil {
		log.Error().Err(err).Msg("list images failed")
		return
	}
	containers, _ := c.runtime.ListContainers(c.ctx)
	used := map[string]struct{}{}
	for _, ct := range containers {
		if ct.ImageID != "" {
			used[ct.ImageID] = struct{}{}
		}
	}

	// compute unused bytes from runtime listing
	imageSizes := map[string]int64{}
	unusedTotal := int64(0)
	for _, img := range imgs {
		imageSizes[img.ID] = img.Size
		if _, ok := used[img.ID]; !ok {
			unusedTotal += img.Size
		}
	}
	log.Info().Int64("unused_bytes", unusedTotal).Msg("unused computed")
	if unusedTotal <= c.maxUnused {
		return
	}

	allMeta, err := c.db.GetAll()
	if err != nil {
		log.Error().Err(err).Msg("db read failed")
		return
	}

	// build layer refcounts and per-image layer lists
	layerRef := map[string]int{}
	imgLayers := map[string][]string{}
	for id, m := range allMeta {
		if len(m.Layers) == 0 {
			continue
		}
		imgLayers[id] = m.Layers
		for _, l := range m.Layers {
			layerRef[l] = layerRef[l] + 1
		}
	}

	// build candidate list (exclude in-use and keep-labeled)
	type cand struct {
		id      string
		display string
		labels  string
		size    int64
		last    int64
	}
	cands := []cand{}
	for id, m := range allMeta {
		if _, inUse := used[id]; inUse {
			continue
		}
		if c.keepLabel != "" && hasKeepLabel(m.Labels, c.keepLabel) {
			continue
		}
		cands = append(cands, cand{
			id:      id,
			size:    m.Size,
			last:    m.LastUsedTs,
			display: m.DisplayName,
			labels:  m.Labels,
		})
	}
	// LRU order
	sort.Slice(cands, func(i, j int) bool { return cands[i].last < cands[j].last })

	// partition leaves (images with at least one unique layer) first
	leaves := []cand{}
	nonLeaves := []cand{}
	for _, x := range cands {
		unique := false
		for _, l := range imgLayers[x.id] {
			if layerRef[l] == 1 {
				unique = true
				break
			}
		}
		if unique {
			leaves = append(leaves, x)
		} else {
			nonLeaves = append(nonLeaves, x)
		}
	}
	ordered := append(leaves, nonLeaves...)

	// delete up to deletionChunk images (or until threshold satisfied)
	deleted := 0
	freed := int64(0)
	for _, x := range ordered {
		if deleted >= c.deletionChunk {
			log.Info().Int("deleted", deleted).Msg("chunk limit reached")
			break
		}
		sz, ok := imageSizes[x.id]
		if !ok {
			// image not present at runtime anymore; drop DB entry
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
			log.Info().
				Str("op", "dry-remove").
				Str("image_id", x.id).
				Str("display", x.display).
				Int64("size", sz).
				Msg("DRY-RUN candidate")
			deleted++
			freed += sz
		} else {
			log.Info().
				Str("op", "remove").
				Str("image_id", x.id).
				Str("display", x.display).
				Int64("size", sz).
				Msg("removing image")
			if err := c.runtime.RemoveImage(c.ctx, x.id); err != nil {
				log.Error().Err(err).Str("image", x.display).Msg("remove failed")
				continue
			}
			_ = c.db.Remove(x.id)
			deleted++
			freed += sz
			if c.deletionSleepMs > 0 {
				time.Sleep(time.Duration(c.deletionSleepMs) * time.Millisecond)
			}
		}
		if unusedTotal-freed <= c.maxUnused {
			break
		}
	}

	log.Info().
		Int("deleted_count", deleted).
		Int64("freed_bytes", freed).
		Msg("reconcile finished")
}

// mustMarshalString returns a compact JSON string for v.
func mustMarshalString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// hasKeepLabel inspects a labels-json string and returns true if keep key is present.
func hasKeepLabel(labels string, keep string) bool {
	if keep == "" || labels == "" {
		return false
	}
	var m map[string]string
	_ = json.Unmarshal([]byte(labels), &m)
	_, ok := m[keep]
	return ok
}
