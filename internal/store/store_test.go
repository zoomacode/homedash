package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRSSRoundtrip(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	item := RSSItem{GUID: "g1", Feed: "f", Title: "T", Link: "L", Published: now, FetchedAt: now}
	if err := db.UpsertRSS(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	got, err := db.RecentRSS(context.Background(), 10)
	if err != nil || len(got) != 1 || got[0].Title != "T" {
		t.Fatalf("got %v %v", got, err)
	}
}
