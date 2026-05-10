package photos

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/store"
)

func TestExtractToken(t *testing.T) {
	tok, err := extractToken("https://www.icloud.com/sharedalbum/#A1B2C3D4")
	if err != nil || tok != "A1B2C3D4" {
		t.Errorf("got %q, %v", tok, err)
	}
}

func TestServerFromToken_Stable(t *testing.T) {
	a := serverFromToken("A1B2C3D4")
	b := serverFromToken("A1B2C3D4")
	if a != b {
		t.Errorf("not deterministic: %s vs %s", a, b)
	}
}

func TestIsLocalPath(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"/var/photos", true},
		{"./photos", true},
		{"https://www.icloud.com/sharedalbum/#X", false},
	}
	for _, c := range cases {
		if got := isLocalPath(c.in); got != c.want {
			t.Errorf("isLocalPath(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestOnceLocal_ScansDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.jpg", "b.png", "c.txt", "d.jpeg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dbPath := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	st := state.New()
	p := &Poller{AlbumURL: "", CacheDir: dir, Store: st, DB: db}
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := st.Snapshot().Photos
	if len(got) != 3 {
		t.Errorf("expected 3 image photos, got %d: %+v", len(got), got)
	}
}

func TestOnceLocal_MissingDir(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	st := state.New()
	p := &Poller{AlbumURL: "/nonexistent/path/no/such/dir", CacheDir: "/nonexistent/path/no/such/dir", Store: st, DB: db}
	if err := p.Once(context.Background()); err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if got := st.Snapshot().Photos; got != nil {
		t.Errorf("expected nil photos, got %+v", got)
	}
}
