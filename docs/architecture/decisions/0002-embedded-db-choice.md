# 2. Embedded DB choice

Date: 2026-02-19

## Status

Accepted

## Context

imgcull requires a small, reliable embedded datastore to persist per-image metadata (image id, repo tags, display name, layers, size, created/last-used timestamps).
Requirements:

- Cross-compile friendly (build single static binary for Windows/linux)
- ACID correctness for small metadata updates
- Simple backup/restore and low operational complexity
- No heavy runtime dependencies or CGO required
- Fast point reads/writes with a small dataset (thousands of images metadata at worst)

## Decision

Choose **bbolt**.

Rationale:

- Matches workload, small ACID metadata store with mainly reads and occasional writes.
- Simplicity outweighs benefits of LSM engines
- Operational simplicitym single file backup/restore, human-understandable size, minimal tuning.
- Is a project under etcd umbrella and, consequently, CNCF and The Linux Foundation almost ensuring long term support

## Consequences

- If dataset grows substantially (millions of entries, unlikely) or write throughput becomes a bottleneck, re-evaluate and consider moving to Pebble or Badger with sharding.
- Design DB schema to keep entries compact and to allow snapshotting the DB file for backup.

## Alternatives considered

1. **SQLite** (via `github.com/mattn/go-sqlite3` or `modernc.org/sqlite`)
   - Pros: familiar SQL, powerful queries, rich tooling.
   - Cons: `mattn/go-sqlite3` requires CGO; `modernc.org/sqlite` avoids CGO but increases binary size; schema migrations add complexity.

2. **Pebble (github.com/cockroachdb/pebble)**
   - Pros: LSM-based, good for write-heavy workloads and large datasets.
   - Cons: heavier runtime, compaction semantics, more operational surface; not required for small metadata.

3. **Badger**
   - Pros: LSM, good throughput.
   - Cons: GC/compaction complexity, higher disk usage, more moving parts.

4. **LevelDB / RocksDB**
   - Pros: mature LSM engines.
   - Cons: CGO or native builds, increased complexity and deployment friction.
