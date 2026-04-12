package db

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestDB_UpsertAndGetAll(t *testing.T) {
	d := openTestDB(t)

	meta := ImageMeta{
		RepoTags:    `["nginx:latest"]`,
		DisplayName: "nginx:latest",
		Size:        1000,
		CreatedTs:   100,
		LastUsedTs:  200,
		Labels:      `{"maintainer":"test"}`,
		Layers:      []string{"sha256:aaa", "sha256:bbb"},
	}
	if err := d.Upsert("img1", meta); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	all, err := d.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
	got := all["img1"]
	if got.DisplayName != "nginx:latest" {
		t.Errorf("DisplayName = %q, want nginx:latest", got.DisplayName)
	}
	if got.Size != 1000 {
		t.Errorf("Size = %d, want 1000", got.Size)
	}
	if got.CreatedTs != 100 {
		t.Errorf("CreatedTs = %d, want 100", got.CreatedTs)
	}
	if got.LastUsedTs != 200 {
		t.Errorf("LastUsedTs = %d, want 200", got.LastUsedTs)
	}
	if len(got.Layers) != 2 {
		t.Fatalf("Layers len = %d, want 2", len(got.Layers))
	}
	if got.Layers[0] != "sha256:aaa" || got.Layers[1] != "sha256:bbb" {
		t.Errorf("Layers = %v, want [sha256:aaa sha256:bbb]", got.Layers)
	}
}

func TestDB_UpsertOverwrite(t *testing.T) {
	d := openTestDB(t)

	meta1 := ImageMeta{DisplayName: "v1", Size: 100}
	if err := d.Upsert("img1", meta1); err != nil {
		t.Fatalf("upsert v1: %v", err)
	}

	meta2 := ImageMeta{DisplayName: "v2", Size: 200}
	if err := d.Upsert("img1", meta2); err != nil {
		t.Fatalf("upsert v2: %v", err)
	}

	all, err := d.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
	if all["img1"].DisplayName != "v2" {
		t.Errorf("DisplayName = %q, want v2", all["img1"].DisplayName)
	}
	if all["img1"].Size != 200 {
		t.Errorf("Size = %d, want 200", all["img1"].Size)
	}
}

func TestDB_SetLastUsed(t *testing.T) {
	d := openTestDB(t)

	meta := ImageMeta{DisplayName: "test", LastUsedTs: 100}
	if err := d.Upsert("img1", meta); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := d.SetLastUsed("img1", 999); err != nil {
		t.Fatalf("set last used: %v", err)
	}

	all, err := d.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if all["img1"].LastUsedTs != 999 {
		t.Errorf("LastUsedTs = %d, want 999", all["img1"].LastUsedTs)
	}
}

func TestDB_SetLastUsed_NonExistent(t *testing.T) {
	d := openTestDB(t)

	if err := d.SetLastUsed("doesnotexist", 999); err != nil {
		t.Fatalf("expected no error for missing key, got: %v", err)
	}

	all, err := d.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty db, got %d entries", len(all))
	}
}

func TestDB_Remove(t *testing.T) {
	d := openTestDB(t)

	if err := d.Upsert("img1", ImageMeta{DisplayName: "keep"}); err != nil {
		t.Fatalf("upsert img1: %v", err)
	}
	if err := d.Upsert("img2", ImageMeta{DisplayName: "delete"}); err != nil {
		t.Fatalf("upsert img2: %v", err)
	}

	if err := d.Remove("img2"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	all, err := d.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
	if _, ok := all["img1"]; !ok {
		t.Error("img1 should still exist")
	}
	if _, ok := all["img2"]; ok {
		t.Error("img2 should be removed")
	}
}

func TestDB_GetAll_EmptyDB(t *testing.T) {
	d := openTestDB(t)

	all, err := d.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty, got %d", len(all))
	}
}
