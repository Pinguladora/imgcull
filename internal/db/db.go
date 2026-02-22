package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

const bucketImages = "images"

// ImageMeta represents the metadata we store for each container image in the DB.
type ImageMeta struct {
	RepoTags    string   `json:"repo_tags"`
	DisplayName string   `json:"display_name"`
	Labels      string   `json:"labels"`
	Layers      []string `json:"layers"`
	Size        int64    `json:"size"`
	CreatedTs   int64    `json:"created_ts"`
	LastUsedTs  int64    `json:"last_used_ts"`
}

// DB is a wrapper around BoltDB for storing image metadata.
type DB struct {
	db   *bolt.DB
	path string
	mu   sync.Mutex
}

// Open opens (or creates) the database at the given path and returns a DB instance.
func Open(path string) (*DB, error) {
	if err := mkdirIfNeeded(path); err != nil {
		return nil, err
	}
	bdb, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}
	if err := bdb.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketImages))
		return err
	}); err != nil {
		bdb.Close()
		return nil, err
	}
	return &DB{path: path, db: bdb}, nil
}

func mkdirIfNeeded(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0o644)
}

// Close closes the database.
func (d *DB) Close() error {
	if err := d.db.Close(); err != nil {
		return err
	}
	return nil
}

// Upsert inserts or updates the image metadata for the given ID.
func (d *DB) Upsert(id string, meta ImageMeta) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketImages))
		buf, _ := json.Marshal(meta)
		return b.Put([]byte(id), buf)
	})
}

// SetLastUsed updates the last used timestamp for the given image ID.
func (d *DB) SetLastUsed(id string, ts int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketImages))
		v := b.Get([]byte(id))
		if v == nil {
			return nil
		}
		var m ImageMeta
		if err := json.Unmarshal(v, &m); err != nil {
			return fmt.Errorf("unmarshal existing meta: %w", err)
		}
		m.LastUsedTs = ts
		buf, _ := json.Marshal(m)
		return b.Put([]byte(id), buf)
	})
}

// Remove deletes the metadata for the given image ID.
func (d *DB) Remove(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketImages))
		return b.Delete([]byte(id))
	})
}

// GetAll returns a map of all image IDs to their metadata.
func (d *DB) GetAll() (map[string]ImageMeta, error) {
	out := map[string]ImageMeta{}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketImages))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var m ImageMeta
			if err := json.Unmarshal(v, &m); err != nil {
				return fmt.Errorf("unmarshal error for image meta %v: %w", m, err)
			}
			out[string(k)] = m
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("get all: %w", err)
	}
	return out, nil
}
