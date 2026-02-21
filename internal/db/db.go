package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

const bucketImages = "images"

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
	bdb, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 1 * time.Second})
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
	return os.MkdirAll(dir, 0o755)
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) Upsert(id string, meta ImageMeta) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketImages))
		buf, _ := json.Marshal(meta)
		return b.Put([]byte(id), buf)
	})
}

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
			return err
		}
		m.LastUsedTs = ts
		buf, _ := json.Marshal(m)
		return b.Put([]byte(id), buf)
	})
}

func (d *DB) Remove(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketImages))
		return b.Delete([]byte(id))
	})
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
				return nil
			}
			out[string(k)] = m
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return out, nil
}
