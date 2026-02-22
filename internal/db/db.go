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

const (
	bucketImages  = "images"
	fileMode      = 0o600
	dirMode       = 0o750
	dbTimeoutSecs = 1
)

type ImageMeta struct {
	RepoTags    string   `json:"repo_tags"`
	DisplayName string   `json:"display_name"`
	Labels      string   `json:"labels"`
	Layers      []string `json:"layers"`
	Size        int64    `json:"size"`
	CreatedTs   int64    `json:"created_ts"`
	LastUsedTs  int64    `json:"last_used_ts"`
}

type DB struct {
	db   *bolt.DB
	path string
	mu   sync.Mutex
}

func Open(path string) (*DB, error) {
	if err := mkdirIfNeeded(path); err != nil {
		return nil, err
	}
	bdb, err := bolt.Open(path, fileMode, &bolt.Options{Timeout: dbTimeoutSecs * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bolt db: %w", err)
	}
	if err := bdb.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketImages))
		if err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
		return nil
	}); err != nil {
		_ = bdb.Close()
		return nil, fmt.Errorf("init bucket: %w", err)
	}
	return &DB{path: path, db: bdb}, nil
}

func mkdirIfNeeded(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create db directory: %w", err)
	}
	return nil
}

func (d *DB) Close() error {
	if err := d.db.Close(); err != nil {
		return fmt.Errorf("close bolt db: %w", err)
	}
	return nil
}

func (d *DB) Upsert(id string, meta ImageMeta) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketImages))
		buf, _ := json.Marshal(meta)
		if err := b.Put([]byte(id), buf); err != nil {
			return fmt.Errorf("put: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("upsert: %w", err)
	}
	return nil
}

func (d *DB) SetLastUsed(id string, ts int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.db.Update(func(tx *bolt.Tx) error {
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
		if err := b.Put([]byte(id), buf); err != nil {
			return fmt.Errorf("put: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("set last used: %w", err)
	}
	return nil
}

func (d *DB) Remove(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketImages))
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	return nil
}

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
