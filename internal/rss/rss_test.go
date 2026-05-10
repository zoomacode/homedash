package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/store"
)

const sampleFeed = `<?xml version="1.0"?>
<rss version="2.0"><channel>
  <title>Test</title>
  <item>
    <title>Hello</title><link>https://example.com/a</link>
    <guid>g1</guid>
    <pubDate>Fri, 09 May 2026 10:00:00 GMT</pubDate>
  </item>
</channel></rss>`

func TestRSS_Once(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	st := state.New()
	p := New([]string{srv.URL}, time.Minute, st, db)
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	news := st.Snapshot().News
	if len(news) != 1 || news[0].Title != "Hello" {
		t.Fatalf("news = %+v", news)
	}
}
