# 3. Layer-aware size accounting

Date: 2026-04-12

## Status

Proposed

## Context

imgcull computes unused image bytes by summing each image's `Size` field from the container runtime CLI (e.g. `docker images --format json`). When multiple images share base layers, this counts shared layer bytes multiple times. The inflated total causes GC to trigger earlier than necessary and delete more images than needed.

imgcull targets broad OCI runtime compatibility (Docker, Podman, nerdctl, CRI-O, containerd, runc, crun, lima). Any solution must work across all runtimes using only data available from standard image inspection.

### Options Considered

**A. `docker system df -v`**: returns `UniqueSize` and `SharedSize` per image directly. One CLI call, no math needed. Rejected because `system df` is a high-level command not available on lower-level runtimes (runc, crun, CRI-O, containerd CLI, lima).

**B. Registry manifest API**: accurate per-layer sizes from OCI manifests. Rejected because it requires registry access and authentication, and does not work for local-only images.

**C. Filesystem introspection**: read layer sizes from the storage driver (e.g. overlay2 directory sizes). Rejected because it is deeply runtime-specific, requires filesystem access, and breaks when imgcull runs in a container.

**D. Proportional estimate from layer digests**: use layer digests already collected via `InspectImage` to determine which layers are shared with in-use images. Estimate unique size as `img.Size * (unique_layers / total_layers)`. Works on any runtime that returns `RootFS.Layers` in inspect output.

## Decision

Use option D: proportional estimate from layer digests.

For each unused image:
```
uniqueLayers = layers NOT present in any in-use image
uniqueRatio  = len(uniqueLayers) / len(allLayers)
uniqueSize   = img.Size * uniqueRatio
```

When `InspectResult.Layers` is empty (runtime did not return layer data), fall back to the full `img.Size`. This preserves current behavior with no regression.

## Consequences

- GC threshold comparison uses deduplicated totals, triggering later and more accurately.
- No new CLI calls required. Layer digests are already collected during `InspectImage`.
- The estimate is an approximation: it assumes layers are roughly equal in size. In practice, base layers tend to be larger than application layers, so the estimate may slightly over- or under-count. This is acceptable for GC threshold decisions.
- Candidate scoring (sorted by `LastUsedTs`) and the existing `partitionByUniqueLayers` logic are unchanged.
- Runtimes that do not return layer digests in inspect output get current behavior automatically.
