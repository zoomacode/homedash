// Package store wraps SQLite for the small bits of homedash that need persistence.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type DB struct{ db *sql.DB }

type RSSItem struct {
	GUID, Feed, Title, Link string
	Published, FetchedAt    time.Time
}

type Photo struct {
	ID, URL, LocalPath string
	FetchedAt          time.Time
}

func Open(path string) (*DB, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := d.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		return nil, fmt.Errorf("pragmas: %w", err)
	}
	if _, err := d.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &DB{db: d}, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) UpsertRSS(ctx context.Context, item RSSItem) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO rss_items(guid, feed, title, link, published, fetched_at)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(guid) DO UPDATE SET title=excluded.title, link=excluded.link, fetched_at=excluded.fetched_at`,
		item.GUID, item.Feed, item.Title, item.Link,
		item.Published.UTC().Format(time.RFC3339),
		item.FetchedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (d *DB) RecentRSS(ctx context.Context, limit int) ([]RSSItem, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT guid, feed, title, link, published, fetched_at FROM rss_items ORDER BY published DESC LIMIT ?`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RSSItem
	for rows.Next() {
		var it RSSItem
		var pub, fet string
		if err := rows.Scan(&it.GUID, &it.Feed, &it.Title, &it.Link, &pub, &fet); err != nil {
			return nil, err
		}
		it.Published, _ = time.Parse(time.RFC3339, pub)
		it.FetchedAt, _ = time.Parse(time.RFC3339, fet)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) UpsertPhoto(ctx context.Context, p Photo) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO photos(id, url, local_path, fetched_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET url=excluded.url, local_path=excluded.local_path, fetched_at=excluded.fetched_at`,
		p.ID, p.URL, p.LocalPath, p.FetchedAt.UTC().Format(time.RFC3339))
	return err
}

